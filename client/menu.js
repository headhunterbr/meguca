
(function () {

$DOC.on('click', '.control', function (event) {
	var $target = $(event.target);
	if ($target.is('li')) {
		console.log($target.text());
	}
	var $menu = $(this).find('ul');
	if ($menu.length)
		$menu.remove();
	else {
		$menu = $('<ul/>');
		var opts = menuOptions;

		/* TODO: Use model lookup */
		var $post = parent_post($target);
		if ($post.length && !$post.attr('id'))
			opts = ['Focus']; /* Just a draft, can't do much */

		_.each(opts, function (opt) {
			$('<li/>').text(opt).appendTo($menu);
		});
		$menu.appendTo(this);
	}
});

$DOC.on('mouseleave', 'ul', function (event) {
	var $ul = $(this);
	if (!$ul.is('ul'))
		return;
	event.stopPropagation();
	var timer = setTimeout(function () {
		/* Using $.proxy() here breaks FF? */
		$ul.remove();
	}, 300);
	/* TODO: Store in view instead */
	$ul.data('closetimer', timer);
});

$DOC.on('mouseenter', 'ul', function (event) {
	var $ul = $(this);
	var timer = $ul.data('closetimer');
	if (timer) {
		clearTimeout(timer);
		$ul.removeData('closetimer');
	}
});

oneeSama.hook('headerFinish', function (info) {
	info.header.unshift(safe('<span class="control"/>'));
});

oneeSama.hook('draft', function ($post) {
	$post.find('header').prepend('<span class=control/>');
});

$('<span class=control/>').prependTo('header');

})();
