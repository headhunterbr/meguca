use crate::{comp_util, state};
use serde::{Deserialize, Serialize};
use yew::{html, Html, Properties};

#[derive(Default)]
pub struct Inner {
	offset: u32,
	page_count: u32,
}

// Used to select a certain page of a thread
pub type PageSelector = comp_util::HookedComponent<Inner>;

#[derive(Clone, Properties, Eq, PartialEq)]
pub struct Props {
	pub thread: u64,
}

#[derive(Serialize, Deserialize, Clone)]
pub enum Message {
	Scroll { left: bool, to_end: bool },
	SelectPage(u32),
	ThreadUpdate,
	NOP,
}

impl comp_util::Inner for Inner {
	type Message = Message;
	type Properties = Props;

	fn init<'a>(&mut self, c: comp_util::Ctx<'a, Self>) {
		self.fetch_page_count(c.props.thread);
	}

	fn update_message() -> Self::Message {
		Message::ThreadUpdate
	}

	fn subscribe_to(props: &Self::Properties) -> Vec<state::Change> {
		vec![state::Change::Thread(props.thread)]
	}

	fn update<'a>(
		&mut self,
		c: comp_util::Ctx<'a, Self>,
		msg: Self::Message,
	) -> bool {
		use Message::*;

		match msg {
			Scroll { left, to_end } => {
				let old = self.offset;
				let max = if self.page_count > 5 {
					self.page_count - 5
				} else {
					0
				};

				if to_end {
					self.offset = if left { 0 } else { max };
				} else {
					if left {
						if self.offset > 0 {
							self.page_count -= 1;
						}
					} else {
						if self.offset < max {
							self.offset += 1;
						}
					}
				}

				self.offset != old
			}
			SelectPage(_) => todo!("page navigation"),
			ThreadUpdate => {
				let old = self.page_count;
				self.fetch_page_count(c.props.thread);
				old != self.page_count
			}
			NOP => false,
		}
	}

	fn view<'a>(&self, c: comp_util::Ctx<'a, Self>) -> Html {
		html! {
			<span class="spaced mono no-select">
				{self.render_scroll_button(&c, "<<", Message::Scroll{
					left: true,
					to_end: true,
				})}
				{
					if self.page_count > 5 {
						self.render_scroll_button(&c, "<", Message::Scroll{
							left: true,
							to_end: false,
						})
					} else {
						html! {}
					}
				}
				{
					for (self.offset..self.page_count).map(|i| html! {
						<a
							onclick=c.link.callback(move |_|
								Message::SelectPage(i)
							)
						>
							{i}
						</a>
					})
				}
				{
					if self.page_count > 5 {
						self.render_scroll_button(&c, ">", Message::Scroll{
							left: false,
							to_end: false,
						})
					} else {
						html! {}
					}
				}
				{self.render_scroll_button(&c, ">>", Message::Scroll{
					left: false,
					to_end: true,
				})}
			</span>
		}
	}
}

impl Inner {
	fn render_scroll_button<'a>(
		&self,
		c: &comp_util::Ctx<'a, Self>,
		text: &str,
		msg: Message,
	) -> Html {
		html! {
			<a onclick=c.link.callback(move |_| msg.clone())>{text}</a>
		}
	}

	// Fetch and set new page count value for thread from global state
	fn fetch_page_count(&mut self, thread: u64) {
		self.page_count = state::read(|s| {
			s.threads.get(&thread).map(|t| t.last_page + 1).unwrap_or(1)
		});
	}
}
