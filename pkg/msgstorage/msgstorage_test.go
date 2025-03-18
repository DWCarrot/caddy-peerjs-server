package msgstorage

import (
	"testing"
	"time"

	"github.com/DWCarrot/caddy-peerjs-server/pkg/utils"
)

type sctTestMessage struct {
	Src string
	Tgt string
	Ddl time.Time
}

// GetExpireTime implements IMessage.
func (s *sctTestMessage) GetExpireTime() time.Time {
	return s.Ddl
}

// GetSource implements IMessage.
func (s *sctTestMessage) GetSource() string {
	return s.Src
}

// GetTarget implements IMessage.
func (s *sctTestMessage) GetTarget() string {
	return s.Tgt
}

func TestMessageStorage(t *testing.T) {

	comp := func(result *sctMessageList, link fnLink, except []IMessage) bool {
		if result == nil {
			if except == nil {
				return true
			}
			t.Errorf("result == nil")
			return false
		}
		var i int = 0
		for e := link(&result.root).next; e != &result.root; e = link(e).next {
			if i >= len(except) {
				t.Errorf("i >= len(except) %d >= %d", i, len(except))
				return false
			}
			r := e.Message
			if r != except[i] {
				t.Errorf("r != except[%d] %v != %v", i, r, except[i])
				return false
			}
			i++
		}
		if i != len(except) {
			t.Errorf("count != len(except) %d != %d", i, len(except))
			return false
		}
		return true
	}

	comp2 := func(result utils.Iterable[IMessage], except []IMessage) bool {
		if result == nil {
			if except == nil {
				return true
			}
			t.Errorf("result == nil")
			return false
		}
		var i int = 0
		for r := range result.Iterator() {
			if i >= len(except) {
				t.Errorf("i >= len(except) %d >= %d", i, len(except))
				return false
			}
			if r != except[i] {
				t.Errorf("r != except[%d] %v != %v", i, r, except[i])
				return false
			}
			i++
		}
		if i != len(except) {
			t.Errorf("count != len(except) %d != %d", i, len(except))
			return false
		}
		return true
	}

	s := NewMessageStorage(4)
	now, _ := time.Parse(time.RFC3339, "2020-01-01T00:00:00Z")

	testAdd := func(m IMessage, of IMessage) {
		o, e := s.Store0(m)
		if e != nil {
			t.Errorf("error: %s", e.Error())
		}
		if o != of {
			t.Errorf("expected: %v, actual: %v", of, o)
		}
	}

	m1 := &sctTestMessage{"a", "b", now.Add(time.Second * 1)}
	m2 := &sctTestMessage{"a", "b", now.Add(time.Second * 2)}
	m3 := &sctTestMessage{"a", "b", now.Add(time.Second * 6)}
	m4 := &sctTestMessage{"a", "c", now.Add(time.Second * 4)}
	m5 := &sctTestMessage{"a", "c", now.Add(time.Second * 5)}

	testAdd(m1, nil)
	testAdd(m2, nil)
	testAdd(m3, nil)
	testAdd(m4, nil)
	testAdd(m5, m1)
	comp(s.src["a"], srcLink, []IMessage{m2, m3, m4, m5})
	comp(s.tgt["b"], tgtLink, []IMessage{m2, m3})
	comp(s.tgt["c"], tgtLink, []IMessage{m4, m5})
	comp(s.ttl, ttlLink, []IMessage{m2, m4, m5, m3})

	m6 := &sctTestMessage{"c", "a", now.Add(time.Millisecond * 1500)}
	m7 := &sctTestMessage{"c", "b", now.Add(time.Millisecond * 2500)}
	m8 := &sctTestMessage{"c", "b", now.Add(time.Millisecond * 3500)}
	m9 := &sctTestMessage{"c", "b", now.Add(time.Millisecond * 4500)}
	m10 := &sctTestMessage{"c", "a", now.Add(time.Millisecond * 6500)}

	testAdd(m6, nil)
	testAdd(m7, nil)
	testAdd(m8, nil)
	testAdd(m9, nil)
	testAdd(m10, m6)

	comp(s.src["c"], srcLink, []IMessage{m7, m8, m9, m10})
	comp(s.tgt["a"], tgtLink, []IMessage{m10})
	comp(s.tgt["b"], tgtLink, []IMessage{m2, m3, m7, m8, m9})
	comp(s.ttl, ttlLink, []IMessage{m2, m7, m8, m4, m9, m5, m3, m10})

	checkR, e := s.Check0(now.Add(time.Millisecond * 3500))
	if e != nil {
		t.Errorf("error: %s", e.Error())
	}
	comp2(checkR["a"], []IMessage{m2})
	comp2(checkR["b"], nil)
	comp2(checkR["c"], []IMessage{m7, m8})
	comp(s.ttl, ttlLink, []IMessage{m4, m9, m5, m3, m10})

	takeR, e := s.Take0("c")
	if e != nil {
		t.Errorf("error: %s", e.Error())
	}
	comp2(takeR, []IMessage{m4, m5})
	comp(s.src["a"], srcLink, []IMessage{m3})
	comp(s.src["b"], srcLink, nil)
	comp(s.ttl, ttlLink, []IMessage{m9, m3, m10})

	checkR, e = s.Check0(now.Add(time.Millisecond * 6000))
	if e != nil {
		t.Errorf("error: %s", e.Error())
	}
	comp2(checkR["a"], []IMessage{m3})
	comp2(checkR["b"], nil)
	comp2(checkR["c"], []IMessage{m9})
	comp(s.src["a"], srcLink, nil)
	comp(s.src["b"], srcLink, nil)
	comp(s.src["c"], srcLink, []IMessage{m10})
	comp(s.ttl, ttlLink, []IMessage{m10})

	m11 := &sctTestMessage{"b", "a", now.Add(time.Millisecond * 7500)}
	m12 := &sctTestMessage{"b", "a", now.Add(time.Millisecond * 8500)}
	m13 := &sctTestMessage{"b", "a", now.Add(time.Millisecond * 9500)}
	m14 := &sctTestMessage{"b", "a", now.Add(time.Millisecond * 10500)}
	m15 := &sctTestMessage{"b", "c", now.Add(time.Millisecond * 11500)}

	testAdd(m11, nil)
	testAdd(m12, nil)
	testAdd(m13, nil)
	testAdd(m14, nil)
	testAdd(m15, m11)

	comp(s.src["b"], srcLink, []IMessage{m12, m13, m14, m15})

	e = s.Drop0("b")
	if e != nil {
		t.Errorf("error: %s", e.Error())
	}

	comp(s.src["b"], srcLink, nil)
	comp(s.tgt["a"], tgtLink, []IMessage{m10})
	comp(s.ttl, ttlLink, []IMessage{m10})
}

var (
	_ IMessage = (*sctTestMessage)(nil)
)
