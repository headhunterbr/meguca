package db

import (
	"time"

	"github.com/bakape/meguca/auth"
	"github.com/bakape/meguca/types"
	r "github.com/dancannon/gorethink"

	. "gopkg.in/check.v1"
)

func (*DBSuite) TestSessionCleanup(c *C) {
	expired := time.Now().Add(-time.Hour)
	samples := []auth.User{
		{
			ID: "1",
			Sessions: []auth.Session{
				{
					Token:   "foo",
					Expires: expired,
				},
				{
					Token:   "bar",
					Expires: time.Now().Add(time.Hour),
				},
			},
		},
		{
			ID: "2",
			Sessions: []auth.Session{
				{
					Token:   "baz",
					Expires: expired,
				},
			},
		},
	}
	c.Assert(Write(r.Table("accounts").Insert(samples)), IsNil)

	expireUserSessions()

	var res1 []auth.Session
	c.Assert(All(GetAccount("1").Field("sessions"), &res1), IsNil)
	c.Assert(len(res1), Equals, 1)
	c.Assert(res1[0].Token, Equals, "bar")

	var res2 []auth.Session
	c.Assert(All(GetAccount("2").Field("sessions"), &res1), IsNil)
	c.Assert(res2, DeepEquals, []auth.Session(nil))
}

func (*DBSuite) TestOpenPostClosing(c *C) {
	thread := types.DatabaseThread{
		ID: 1,
		Posts: map[int64]types.DatabasePost{
			1: {
				Post: types.Post{
					ID:      1,
					Editing: true,
					Time:    time.Now().Add(-time.Minute * 31).Unix(),
				},
				Password: []byte{},
			},
			2: {
				Post: types.Post{
					ID:      2,
					Editing: true,
					Time:    time.Now().Unix(),
				},
				Password: []byte{},
			},
		},
		Log: [][]byte{[]byte{1, 22, 3}},
	}
	c.Assert(Write(r.Table("threads").Insert(thread)), IsNil)

	closeDanglingPosts()

	var log [][]byte
	c.Assert(All(r.Table("threads").Get(1).Field("log"), &log), IsNil)
	c.Assert(log, DeepEquals, append(thread.Log, []byte("061")))

	std := thread.Posts[1]
	std.Editing = false
	samples := [...]struct {
		id  int64
		std types.DatabasePost
	}{
		{1, std},
		{2, thread.Posts[2]},
	}
	for _, s := range samples {
		var res types.DatabasePost
		c.Assert(One(FindPost(s.id), &res), IsNil)
		c.Assert(res, DeepEquals, s.std)
	}
}