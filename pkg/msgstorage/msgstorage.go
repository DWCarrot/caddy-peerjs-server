package messagestorage

import (
	"iter"
	"sync"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
)

type IMessage interface {

	// Get the source id of the message.
	// this call should be constant for this message instance.
	GetSource() string

	// Get the target id of the message.
	// this call should be constant for this message instance.
	GetTarget() string

	// Get the expiration time of the message.
	// this call should be constant for this message instance.
	GetExpireTime() time.Time
}

type sctMessageElement struct {
	Message IMessage
	lnSrc   sctMessageElementLink
	lnTgt   sctMessageElementLink
	lnTTL   sctMessageElementLink
	next    *sctMessageElement
}

type sctMessageElementLink struct {
	list *sctMessageList
	next *sctMessageElement
	priv *sctMessageElement
}

type fnLink func(*sctMessageElement) *sctMessageElementLink

type fnPredicate func(*sctMessageElement) bool

func srcLink(e *sctMessageElement) *sctMessageElementLink {
	return &e.lnSrc
}

func tgtLink(e *sctMessageElement) *sctMessageElementLink {
	return &e.lnTgt
}

func ttlLink(e *sctMessageElement) *sctMessageElementLink {
	return &e.lnTTL
}

type sctMessageList struct {
	root sctMessageElement
	len  int
}

func newMessageList(link fnLink) *sctMessageList {
	list := &sctMessageList{}
	list.len = 0
	ln := link(&list.root)
	ln.next = &list.root
	ln.priv = &list.root
	return list
}

func (l *sctMessageList) Len() int {
	return l.len
}

func (l *sctMessageList) front(link fnLink) *sctMessageElement {
	if l.len == 0 {
		return nil
	}
	return link(&l.root).next
}

func (l *sctMessageList) doInsertBefore(e *sctMessageElement, at *sctMessageElement, link fnLink) *sctMessageElement {
	lnThis := link(e)
	lnAt := link(at)
	lnAtPriv := link(lnAt.priv)
	lnThis.next = at
	lnThis.priv = lnAt.priv
	lnAtPriv.next = e
	lnAt.priv = e
	l.len++
	lnThis.list = l
	return e
}

func (l *sctMessageList) findLastBefore(predicate fnPredicate, link fnLink) *sctMessageElement {
	var result *sctMessageElement = nil
	for e := link(&l.root).priv; e != &l.root; e = link(e).priv {
		if predicate(e) {
			break
		}
		result = e
	}
	return result
}

func (e *sctMessageElement) remove() (int, int, int) {
	var srcListLen int = -1
	var tgtListLen int = -1
	var ttlListLen int = -1
	if e.lnSrc.list != nil {
		e.lnSrc.next.lnSrc.priv = e.lnSrc.priv
		e.lnSrc.priv.lnSrc.next = e.lnSrc.next
		e.lnSrc.list.len--
		srcListLen = e.lnSrc.list.len
		e.lnSrc.list = nil
		e.lnSrc.next = nil
		e.lnSrc.priv = nil
	}
	if e.lnTgt.list != nil {
		e.lnTgt.next.lnTgt.priv = e.lnTgt.priv
		e.lnTgt.priv.lnTgt.next = e.lnTgt.next
		e.lnTgt.list.len--
		tgtListLen = e.lnTgt.list.len
		e.lnTgt.list = nil
		e.lnTgt.next = nil
		e.lnTgt.priv = nil
	}
	if e.lnTTL.list != nil {
		e.lnTTL.next.lnTTL.priv = e.lnTTL.priv
		e.lnTTL.priv.lnTTL.next = e.lnTTL.next
		e.lnTTL.list.len--
		ttlListLen = e.lnTTL.list.len
		e.lnTTL.list = nil
		e.lnTTL.next = nil
		e.lnTTL.priv = nil
	}
	return srcListLen, tgtListLen, ttlListLen
}

type sctCollection struct {
	head *sctMessageElement
	tail *sctMessageElement
}

func (c *sctCollection) Add(e *sctMessageElement) {
	if c.head == nil {
		c.head = e
	} else {
		c.tail.next = e
	}
	c.tail = e
}

func (c sctCollection) Iterator() iter.Seq[IMessage] {
	root := c.head
	return func(yield func(IMessage) bool) {
		for e := root; e != nil; e = e.next {
			if !yield(e.Message) {
				break
			}
		}
	}
}

// MessageStorage is a storage for messages.
type MessageStorage struct {
	limit uint
	src   map[string]*sctMessageList
	tgt   map[string]*sctMessageList
	ttl   *sctMessageList
	mu    sync.Mutex
}

func NewMessageStorage(limitPerSrc uint) *MessageStorage {
	return &MessageStorage{
		limit: limitPerSrc,
		src:   make(map[string]*sctMessageList),
		tgt:   make(map[string]*sctMessageList),
		ttl:   newMessageList(ttlLink),
		mu:    sync.Mutex{},
	}
}

// Store a message for a client; with lock.
// Returns the overflown message and the expiration time
// Returns an error if the message could not be stored.
func (m *MessageStorage) Store(msg IMessage) (IMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Store0(msg)
}

// Store a message for a client.
// Returns the overflown message and the expiration time
// Returns an error if the message could not be stored.
func (m *MessageStorage) Store0(msg IMessage) (IMessage, error) {
	srcId := msg.GetSource()
	qSrc, ok := m.src[srcId]
	if !ok {
		qSrc = newMessageList(srcLink)
		m.src[srcId] = qSrc
	}

	var overflown *sctMessageElement = nil
	if uint(qSrc.Len()) >= m.limit {
		overflown = qSrc.front(srcLink)
		_, tgtListLen, _ := overflown.remove()
		if tgtListLen == 0 {
			overflownTgtId := overflown.Message.GetTarget()
			delete(m.tgt, overflownTgtId)
		}
	}

	e := &sctMessageElement{Message: msg}
	qSrc.doInsertBefore(e, &qSrc.root, srcLink)

	tgtId := msg.GetTarget()
	qTgt, ok := m.tgt[tgtId]
	if !ok {
		qTgt = newMessageList(tgtLink)
		m.tgt[tgtId] = qTgt
	}
	qTgt.doInsertBefore(e, &qTgt.root, tgtLink)

	expire := msg.GetExpireTime()
	pred := func(e *sctMessageElement) bool {
		return !e.Message.GetExpireTime().After(expire)
	}
	pos := m.ttl.findLastBefore(pred, ttlLink)
	if pos == nil {
		m.ttl.doInsertBefore(e, &m.ttl.root, ttlLink)
	} else {
		m.ttl.doInsertBefore(e, pos, ttlLink)
	}

	if overflown == nil {
		return nil, nil
	}
	return overflown.Message, nil
}

// Drop0 all messages for a specific client (with source id); with lock.
// Returns an error if the messages could not be dropped.
func (m *MessageStorage) Drop(srcId string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Drop0(srcId)
}

// Drop0 all messages for a specific client (with source id).
// Returns an error if the messages could not be dropped.
func (m *MessageStorage) Drop0(srcId string) error {
	qSrc, ok := m.src[srcId]
	if !ok {
		return nil
	}
	for e := qSrc.front(srcLink); e != nil; e = qSrc.front(srcLink) {
		_, tgtListLen, _ := e.remove()
		if tgtListLen == 0 {
			tgtId := e.Message.GetTarget()
			delete(m.tgt, tgtId)
		}
	}
	delete(m.src, srcId)
	return nil
}

// Take all valid messages for a specific client (with target id); with lock.
// Returns a slice of messages.
// Returns an error if the messages could not be retrieved.
func (m *MessageStorage) Take(tgtId string) (utils.Iterable[IMessage], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Take0(tgtId)
}

// Take all valid messages for a specific client (with target id).
// Returns a slice of messages.
// Returns an error if the messages could not be retrieved.
func (m *MessageStorage) Take0(tgtId string) (utils.Iterable[IMessage], error) {
	result := sctCollection{}
	qTgt, ok := m.tgt[tgtId]
	if ok {
		for e := qTgt.front(tgtLink); e != nil; e = qTgt.front(tgtLink) {
			srcLen, _, _ := e.remove()
			if srcLen == 0 {
				srcId := e.Message.GetSource()
				delete(m.src, srcId)
			}
			result.Add(e)
		}
		delete(m.tgt, tgtId)
	}

	return result, nil
}

// Check the storage for expired (expire <= deadline) messages; with lock.
// Returns a map of expired messages for each client, grouped by source id.
// Returns an error if the messages could not be checked.
func (m *MessageStorage) Check(ddl time.Time) (map[string]utils.Iterable[IMessage], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Check0(ddl)
}

// Check0 the storage for expired (expire <= deadline) messages.
// Returns a map of expired messages for each client, grouped by source id.
// Returns an error if the messages could not be checked.
func (m *MessageStorage) Check0(ddl time.Time) (map[string]utils.Iterable[IMessage], error) {
	collections := make(map[string]utils.Iterable[IMessage])
	for e := m.ttl.front(ttlLink); e != nil; e = m.ttl.front(ttlLink) {
		if e.Message.GetExpireTime().After(ddl) {
			break
		}
		srcLen, tgtListLen, _ := e.remove()
		srcId := e.Message.GetSource()
		if srcLen == 0 {
			delete(m.src, srcId)
		}
		if tgtListLen == 0 {
			tgtId := e.Message.GetTarget()
			delete(m.tgt, tgtId)
		}
		collection, ok := collections[srcId]
		var cc *sctCollection
		if !ok {
			cc = &sctCollection{}
			collections[srcId] = cc
		} else {
			cc = collection.(*sctCollection)
		}
		cc.Add(e)
	}
	return collections, nil
}

func (m *MessageStorage) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.src = make(map[string]*sctMessageList)
	m.tgt = make(map[string]*sctMessageList)
	m.ttl = newMessageList(ttlLink)
}

var (
	_ utils.Iterable[IMessage] = (*sctCollection)(nil)
)
