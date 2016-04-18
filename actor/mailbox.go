package actor

import "sync/atomic"

const (
	MailboxIdle    = iota
	MailboxRunning = iota
)
const (
	MailboxHasNoMessages   = iota
	MailboxHasMoreMessages = iota
)

type Mailbox interface {
	PostUserMessage(message interface{})
	PostSystemMessage(message interface{})
}

type DefaultMailbox struct {
	userMailbox     chan interface{}
	systemMailbox   chan interface{}
	schedulerStatus int32
	hasMoreMessages int32
	actorCell       *ActorCell
}

func (mailbox *DefaultMailbox) PostUserMessage(message interface{}) {
	mailbox.userMailbox <- message
	mailbox.schedule()
}

func (mailbox *DefaultMailbox) PostSystemMessage(message interface{}) {
	mailbox.systemMailbox <- message
	mailbox.schedule()
}

func (mailbox *DefaultMailbox) schedule() {
	swapped := atomic.CompareAndSwapInt32(&mailbox.schedulerStatus, MailboxIdle, MailboxRunning)
	atomic.StoreInt32(&mailbox.hasMoreMessages, MailboxHasMoreMessages) //we have more messages to process
	if swapped {
		go mailbox.processMessages()
	}
}

func (mailbox *DefaultMailbox) processMessages() {
	//we are about to start processing messages, we can safely reset the message flag of the mailbox
	atomic.StoreInt32(&mailbox.hasMoreMessages, MailboxHasNoMessages)

	//process x messages in sequence, then exit
	for i := 0; i < 30; i++ {
		select {
		case sysMsg := <-mailbox.systemMailbox:
			//prioritize system messages
			mailbox.actorCell.invokeSystemMessage(sysMsg)
		default:
			//if no system message is present, try read user message
			select {
			case userMsg := <-mailbox.userMailbox:
				mailbox.actorCell.invokeUserMessage(userMsg)
			default:
			}
		}
	}
	//set mailbox to idle
	atomic.StoreInt32(&mailbox.schedulerStatus, MailboxIdle)
	//check if there are still messages to process (sent after the message loop ended)
	hasMore := atomic.LoadInt32(&mailbox.hasMoreMessages)
	//what is the current status of the mailbox? it could have changed concurrently since the last two lines
	status := atomic.LoadInt32(&mailbox.schedulerStatus)
	//if there are still messages to process and the mailbox is idle, then reschedule a mailbox run
	//otherwise, we either exit, or the mailbox have been scheduled already by the schedule method
	if hasMore == MailboxHasMoreMessages && status == MailboxIdle {
		swapped := atomic.CompareAndSwapInt32(&mailbox.schedulerStatus, MailboxIdle, MailboxRunning)
		if swapped {
			go mailbox.processMessages()
		}
	}
}

func NewDefaultMailbox(cell *ActorCell) Mailbox {
	userMailbox := make(chan interface{}, 100)
	systemMailbox := make(chan interface{}, 100)
	mailbox := DefaultMailbox{
		userMailbox:     userMailbox,
		systemMailbox:   systemMailbox,
		hasMoreMessages: MailboxHasNoMessages,
		schedulerStatus: MailboxIdle,
		actorCell:       cell,
	}
	return &mailbox
}