package websockets

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/bakape/meguca/auth"
	"github.com/bakape/meguca/config"
	"github.com/bakape/meguca/db"
	"github.com/bakape/meguca/imager"
	"github.com/bakape/meguca/parser"
	"github.com/bakape/meguca/types"
	"github.com/bakape/meguca/util"
	r "github.com/dancannon/gorethink"
)

var (
	errReadOnly          = errors.New("read only board")
	errInvalidImageToken = errors.New("invalid image token")
	errNoImageName       = errors.New("no image name")
	errImageNameTooLong  = errors.New("image name too long")
	errNoTextOrImage     = errors.New("no text or image")
	errThreadIsLocked    = errors.New("thread is locked")
)

// Websocket message response codes
const (
	postCreated = iota
	invalidInsertionCaptcha
)

type threadCreationRequest struct {
	postCreationCommon
	Subject string `json:"subject"`
	Board   string `json:"board"`
	types.Captcha
}

type postCreationCommon struct {
	Image    imageRequest `json:"image"`
	Name     string       `json:"name"`
	Email    string       `json:"email"`
	Auth     string       `json:"auth"`
	Password string       `json:"password"`
}

type imageRequest struct {
	Spoiler bool   `json:"spoiler"`
	Token   string `json:"token"`
	Name    string `json:"name"`
}

type replyCreationRequest struct {
	postCreationCommon
	Body string `json:"body"`
}

// Response to a thread creation request
type threadCreationResponse struct {
	Code int   `json:"code"`
	ID   int64 `json:"id"`
}

// Insert a new thread into the database
func insertThread(data []byte, c *Client) (err error) {
	if err := closePreviousPost(c); err != nil {
		return err
	}
	var req threadCreationRequest
	if err := decodeMessage(data, &req); err != nil {
		return err
	}
	if !auth.IsNonMetaBoard(req.Board) {
		return errInvalidBoard
	}
	if !authenticateCaptcha(req.Captcha, c.IP) {
		return c.sendMessage(messageInsertThread, threadCreationResponse{
			Code: invalidInsertionCaptcha,
		})
	}

	conf, err := getBoardConfig(req.Board)
	if err != nil {
		return err
	}

	post, now, err := constructPost(req.postCreationCommon, c)
	if err != nil {
		return err
	}
	thread := types.DatabaseThread{
		BumpTime:  now,
		ReplyTime: now,
		Board:     req.Board,
	}
	thread.Subject, err = parser.ParseSubject(req.Subject)
	if err != nil {
		return err
	}

	// Perform this last, so there are less dangling images because of an error
	if !conf.TextOnly {
		img := req.Image
		post.Image, err = getImage(img.Token, img.Name, img.Spoiler)
		thread.ImageCtr = 1
		if err != nil {
			return err
		}
	}

	id, err := db.ReservePostID()
	if err != nil {
		return err
	}
	thread.ID = id
	post.ID = id
	thread.Posts = map[int64]types.DatabasePost{
		id: post,
	}

	if err := db.Write(r.Table("threads").Insert(thread)); err != nil {
		return err
	}
	if err := db.IncrementBoardCounter(req.Board); err != nil {
		return err
	}

	c.openPost = openPost{
		id:       id,
		op:       id,
		time:     now,
		board:    req.Board,
		hasImage: !conf.TextOnly,
	}

	msg := threadCreationResponse{
		Code: postCreated,
		ID:   id,
	}
	return c.sendMessage(messageInsertThread, msg)
}

// Insert a new post into the database
func insertPost(data []byte, c *Client) error {
	if err := closePreviousPost(c); err != nil {
		return err
	}

	var req replyCreationRequest
	if err := decodeMessage(data, &req); err != nil {
		return err
	}

	_, sync := Clients.GetSync(c)
	conf, err := getBoardConfig(sync.Board)
	if err != nil {
		return err
	}

	// Post must have either at least one character or an image to be allocated
	noImage := conf.TextOnly || req.Image.Token == "" || req.Image.Name == ""
	if req.Body == "" && noImage {
		return errNoTextOrImage
	}

	// Check thread is not locked and retrieve the post counter
	var threadAttrs struct {
		Locked  bool `gorethink:"locked"`
		PostCtr int  `gorethink:"postCtr"`
	}
	q := r.Table("threads").Get(sync.OP).Pluck("locked", "postCtr")
	if err := db.One(q, &threadAttrs); err != nil {
		return err
	}
	if threadAttrs.Locked {
		return errThreadIsLocked
	}

	post, now, err := constructPost(req.postCreationCommon, c)
	if err != nil {
		return err
	}

	// If the post contains a newline, slice till it and commit the remainder
	// separatly as a splice
	var forSplicing string
	iNewline := strings.IndexRune(req.Body, '\n')
	if iNewline > -1 {
		forSplicing = req.Body[iNewline+1:]
		req.Body = req.Body[:iNewline]
	}
	post.Body = req.Body

	post.ID, err = db.ReservePostID()
	if err != nil {
		return err
	}

	updates := make(msi, 6)
	updates["postCtr"] = r.Row.Field("postCtr").Add(1)
	updates["replyTime"] = now

	// TODO: More dynamic maximum bump limit generation
	if req.Email != "sage" && threadAttrs.PostCtr < config.Get().MaxBump {
		updates["bumpTime"] = now
	}

	if !noImage {
		img := req.Image
		post.Image, err = getImage(img.Token, img.Name, img.Spoiler)
		if err != nil {
			return err
		}
		updates["imageCtr"] = r.Row.Field("imageCtr").Add(1)
	}

	updates["posts"] = map[string]types.DatabasePost{
		util.IDToString(post.ID): post,
	}

	msg, err := encodeMessage(messageInsertPost, post.Post)
	if err != nil {
		return err
	}
	updates["log"] = r.Row.Field("log").Append(msg)

	q = r.Table("threads").Get(sync.OP).Update(updates)
	if err := db.Write(q); err != nil {
		return err
	}
	if err := db.IncrementBoardCounter(sync.Board); err != nil {
		return err
	}

	c.openPost = openPost{
		id:         post.ID,
		op:         sync.OP,
		time:       now,
		board:      sync.Board,
		Buffer:     *bytes.NewBuffer([]byte(req.Body)),
		bodyLength: len(req.Body),
		hasImage:   !noImage,
	}

	if forSplicing != "" {
		if err := parseLine(c, true); err != nil {
			return err
		}
		return spliceLine(spliceRequest{Text: forSplicing}, c)
	}

	return nil
}

// If the client has a previous post, close it silently
func closePreviousPost(c *Client) error {
	if c.openPost.id != 0 {
		return closePost(nil, c)
	}
	return nil
}

// Reatrieve post-related board configuraions
func getBoardConfig(board string) (conf config.PostParseConfigs, err error) {
	err = db.One(db.GetBoardConfig(board), &conf)
	if err != nil {
		return
	}
	if conf.ReadOnly {
		err = errReadOnly
	}
	return
}

// Contruct the common parts of the new post for both threads and replies
func constructPost(req postCreationCommon, c *Client) (
	post types.DatabasePost, now int64, err error,
) {
	now = time.Now().Unix()
	post = types.DatabasePost{
		Post: types.Post{
			Editing: true,
			Time:    now,
			Email:   parser.FormatEmail(req.Email),
		},
		IP: c.IP,
	}
	post.Name, post.Trip, err = parser.ParseName(req.Name)
	if err != nil {
		return
	}
	err = parser.VerifyPostPassword(req.Password)
	if err != nil {
		return
	}
	post.Password, err = auth.BcryptHash(req.Password, 6)

	// TODO: Staff title verification

	return
}

// Performs some validations and retrieves processed image data by token ID.
// Embeds spoiler and image name in result struct. The last extension is
// stripped from the name.
func getImage(token, name string, spoiler bool) (img *types.Image, err error) {
	switch {
	case len(token) > 127, token == "": // RethinkDB key length limit
		err = errInvalidImageToken
	case name == "":
		err = errNoImageName
	case len(name) > 200:
		err = errImageNameTooLong
	}
	if err != nil {
		return
	}

	imgCommon, err := imager.UseImageToken(token)
	if err != nil {
		if err == imager.ErrInvalidToken {
			err = errInvalidImageToken
		}
		return
	}

	return &types.Image{
		ImageCommon: imgCommon,
		Spoiler:     spoiler,
		Name:        strings.TrimSuffix(name, filepath.Ext(name)),
	}, nil
}