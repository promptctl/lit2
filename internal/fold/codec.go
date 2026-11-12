package fold

import (
	"errors"
	"fmt"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/store"
)

// The snapshot's payload is two wire sections. Section 1 is the verdict
// exceptions under their own checkTag, read by a genesis under any fold
// version, since a verdict depends on the event's bytes and the suites,
// never on the fold. Section 2 is the state, read only under the Tag the
// store keyed the snapshot by. The marks are the store's, never copied in
// here `[LAW:one-source-of-truth]`.
//
//	payload   = 1:checked{1:check tag 2:exception…} 2:state{1:through
//	            2:frontier 3:attribution… 4:item… 5:carried… 6:unapplied…
//	            7:malformed…}
//	item      = 1:id 2:creation{1:stamp 2:container}… 3:tombstone stamp
//	            4:facts 5:element{1:name 2:listing{1:stamp 2:list}… 3:facts}…
//	container = 1:home level | 2:parent id
//	facts     = 1:register{1:name 2:candidate{1:stamp 2:value}… 3:retired…}…
//	            2:set{1:name 2:tally{1:member 2:add stamp… 3:removed…}…}…
//	stamp     = 1:position 2:event id 3:attribution index 4:recorded-at
//	carried   = 1:order key 2:principal 3:recorded-at 4:reason
//	unapplied = 1:stamp 2:kind 3:about{1:item 2:element} 4:why
//	malformed = 1:ref 2:offset 3:digest 4:why
//
// A stamp's writer is its attribution's checkout, and its order key's id
// its event id, so each is stored once.
const checkTag = "lit2 check 1"

// The reasons' wire numbers.
const (
	reasonBadSignature = 1
	reasonUnknownSuite = 2

	whyUnknownKind  = 1
	whyNewerSchema  = 2
	whyUndecodable  = 3
	whyErased       = 4
	whyUncommitted  = 5
	whyMisaddressed = 6
)

// encode is the snapshot as a payload.
func (sn Snapshot) encode() []byte {
	var checked, state, payload wire.Writer
	checked.Bytes(1, []byte(checkTag))
	events.WriteIDs(&checked, 2, sn.Exceptions)
	if u, ok := sn.Through.(Upto); ok {
		state.Bytes(1, u.Key.Wire())
	}
	state.Bytes(2, sn.State.frontier.Wire())
	// every stamp is encoded before the table, which precedes them on the
	// wire so a reader resolves each stamp as it meets it
	enc := encoder{index: map[string]uint64{}}
	var stamped []tagged
	for _, r := range sn.State.records() {
		stamped = append(stamped, tagged{tag: 4, b: enc.item(r)})
	}
	for _, c := range sn.State.integrity.carriedList() {
		var w wire.Writer
		w.Bytes(1, c.order.Wire())
		w.Bytes(2, c.carried.Principal.Wire())
		w.Uvarint(3, uint64(c.carried.RecordedAt.UnixNano()))
		w.Uvarint(4, reasonNumber(c.carried.Reason))
		stamped = append(stamped, tagged{tag: 5, b: w.Encoded()})
	}
	for _, u := range sn.State.integrity.unappliedList() {
		var w wire.Writer
		w.Bytes(1, enc.stamp(u.Stamp))
		w.Bytes(2, []byte(u.Kind))
		w.Bytes(3, events.AboutWire(u.About))
		w.Bytes(4, whyWire(u.Why))
		stamped = append(stamped, tagged{tag: 6, b: w.Encoded()})
	}
	for _, m := range sn.State.integrity.malformedList() {
		var w wire.Writer
		w.Bytes(1, []byte(m.Ref))
		w.Uvarint(2, uint64(m.At))
		w.Bytes(3, m.Digest[:])
		w.Bytes(4, []byte(m.Why))
		stamped = append(stamped, tagged{tag: 7, b: w.Encoded()})
	}
	for _, a := range enc.table {
		state.Bytes(3, a)
	}
	for _, f := range stamped {
		state.Bytes(f.tag, f.b)
	}
	payload.Bytes(1, checked.Encoded())
	payload.Bytes(2, state.Encoded())
	return payload.Encoded()
}

// tagged is one field of the state, encoded and not yet written.
type tagged struct {
	tag uint32
	b   []byte
}

// encoder writes stamps against one table of attributions, each written
// once.
type encoder struct {
	index map[string]uint64
	table [][]byte
}

func (enc *encoder) stamp(s Stamp) []byte {
	a := s.By.Wire()
	i, ok := enc.index[string(a)]
	if !ok {
		i = uint64(len(enc.table))
		enc.index[string(a)] = i
		enc.table = append(enc.table, a)
	}
	var w wire.Writer
	w.Bytes(1, s.Order.Position.Wire())
	w.Bytes(2, s.Event[:])
	w.Uvarint(3, i)
	w.Uvarint(4, uint64(s.RecordedAt.UnixNano()))
	return w.Encoded()
}

func (enc *encoder) item(r *item) []byte {
	var w wire.Writer
	w.Bytes(1, []byte(r.id.String()))
	for _, c := range r.creates {
		var cw wire.Writer
		cw.Bytes(1, enc.stamp(c.stamp))
		// the placement alone: a child's parent is its id's prefix, so it
		// is derived on read and never stored to disagree with it
		cw.Bytes(2, events.PlacementWire(placementOf(c.in)))
		w.Bytes(2, cw.Encoded())
	}
	switch l := r.life.(type) {
	case Tombstoned:
		w.Bytes(3, enc.stamp(l.By))
	case Live:
	}
	w.Bytes(4, enc.facts(r.facts))
	for _, el := range r.elements {
		var ew wire.Writer
		ew.Bytes(1, []byte(el.name.String()))
		for _, l := range el.creates {
			var lw wire.Writer
			lw.Bytes(1, enc.stamp(l.stamp))
			lw.Bytes(2, []byte(l.in.String()))
			ew.Bytes(2, lw.Encoded())
		}
		ew.Bytes(3, enc.facts(el.facts))
		w.Bytes(5, ew.Encoded())
	}
	return w.Encoded()
}

func (enc *encoder) facts(f Facts) []byte {
	var w wire.Writer
	for _, r := range f.fields {
		var rw wire.Writer
		rw.Bytes(1, []byte(r.name.String()))
		for _, c := range r.live {
			var cw wire.Writer
			cw.Bytes(1, enc.stamp(c.stamp))
			cw.Bytes(2, c.value.Wire())
			rw.Bytes(2, cw.Encoded())
		}
		events.WriteIDs(&rw, 3, r.retired)
		w.Bytes(1, rw.Encoded())
	}
	for _, s := range f.sets {
		var sw wire.Writer
		sw.Bytes(1, []byte(s.name.String()))
		for _, t := range s.members {
			var tw wire.Writer
			tw.Bytes(1, []byte(t.name.String()))
			for _, a := range t.adds {
				tw.Bytes(2, enc.stamp(a))
			}
			events.WriteIDs(&tw, 3, t.removed)
			sw.Bytes(2, tw.Encoded())
		}
		w.Bytes(2, sw.Encoded())
	}
	return w.Encoded()
}

func whyWire(why UnappliedWhy) []byte {
	var w wire.Writer
	switch why := why.(type) {
	case Unread:
		switch u := why.Why.(type) {
		case events.UnknownKind:
			w.Bytes(whyUnknownKind, nil)
		case events.NewerSchema:
			var n wire.Writer
			n.Uvarint(1, uint64(u.Have))
			n.Uvarint(2, uint64(u.Max))
			w.Bytes(whyNewerSchema, n.Encoded())
		case events.Undecodable:
			w.Bytes(whyUndecodable, []byte(u.Err.Error()))
		}
	case Erased:
		w.Bytes(whyErased, nil)
	case Uncommitted:
		w.Bytes(whyUncommitted, nil)
	case Misaddressed:
		w.Bytes(whyMisaddressed, []byte(why.Want))
	}
	return w.Encoded()
}

func reasonNumber(r Reason) uint64 {
	switch r.(type) {
	case BadSignature:
		return reasonBadSignature
	case UnknownSuite:
		return reasonUnknownSuite
	}
	panic(fmt.Sprintf("fold: %T is not a Reason", r))
}

// ReadSnapshot reads a payload this fold's Tag wrote
// `[LAW:parse-dont-validate]`: the root resumes from the Snapshot, or
// plans again with the snapshot torn for the error.
func ReadSnapshot(payload []byte) (Snapshot, error) {
	var sn Snapshot
	var checked, state []byte
	var hasChecked, hasState bool
	err := wire.NewReader(payload).Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			checked, hasChecked = r.Bytes(), true
		case 2:
			state, hasState = r.Bytes(), true
		}
		return nil
	})
	switch {
	case err != nil:
		return Snapshot{}, fmt.Errorf("the fold's payload: %w", err)
	case !hasChecked || !hasState:
		return Snapshot{}, errors.New("the fold's payload is not its two sections")
	}
	if sn.Exceptions, err = exceptionsOf(checked); err != nil {
		return Snapshot{}, err
	}
	if sn.Through, sn.State, err = readState(state); err != nil {
		return Snapshot{}, fmt.Errorf("the fold's state: %w", err)
	}
	return sn, nil
}

// readExceptions reads section 1 of a payload any fold version wrote.
func readExceptions(payload []byte) ([]events.EventID, error) {
	var checked []byte
	var has bool
	err := wire.NewReader(payload).Each(func(tag uint32, r *wire.Reader) error {
		if tag == 1 {
			checked, has = r.Bytes(), true
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("the fold's payload: %w", err)
	case !has:
		return nil, errors.New("the fold's payload has no verdicts")
	}
	return exceptionsOf(checked)
}

func exceptionsOf(checked []byte) ([]events.EventID, error) {
	var tag string
	var ex []events.EventID
	err := wire.NewReader(checked).Each(func(t uint32, r *wire.Reader) error {
		switch t {
		case 1:
			tag = string(r.Bytes())
		case 2:
			id, err := events.ReadEventID(r.Bytes())
			ex = append(ex, id)
			return err
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("the fold's verdicts: %w", err)
	case tag != checkTag:
		return nil, fmt.Errorf("the fold's verdicts are under %q, not %q", tag, checkTag)
	}
	return withIDs(nil, ex), nil
}

// decoder reads stamps against the payload's table of attributions.
type decoder struct{ table []events.Attribution }

func readState(b []byte) (Through, State, error) {
	var through Through = Nothing{}
	var s State
	var dec decoder
	var hasFrontier bool
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			var k events.OrderKey
			k, err = events.ReadOrderKey(r.Bytes())
			through = Upto{Key: k}
		case 2:
			s.frontier, err = events.ReadFrontier(r.Bytes())
			hasFrontier = true
		case 3:
			var a events.Attribution
			a, err = events.ReadAttribution(r.Bytes())
			dec.table = append(dec.table, a)
		case 4:
			var it *item
			it, err = dec.item(r.Bytes())
			if err == nil {
				s.items = s.items.set(it)
			}
		case 5:
			var c carriedAt
			c, err = readCarried(r.Bytes())
			s.integrity.carried = s.integrity.carried.set(c)
		case 6:
			var u Unapplied
			u, err = dec.unapplied(r.Bytes())
			s.integrity.unapplied = s.integrity.unapplied.set(u)
		case 7:
			var m Malformed
			m, err = readMalformed(r.Bytes())
			s.integrity.malformed = s.integrity.malformed.set(m)
		}
		return err
	})
	switch {
	case err != nil:
		return nil, State{}, err
	case !hasFrontier:
		return nil, State{}, errors.New("no frontier")
	}
	return through, s, nil
}

func (dec decoder) stamp(b []byte) (Stamp, error) {
	var s Stamp
	var seen [5]bool
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			s.Order.Position, err = events.ReadPosition(r.Bytes())
		case 2:
			s.Event, err = events.ReadEventID(r.Bytes())
		case 3:
			i := r.Uvarint()
			if i >= uint64(len(dec.table)) {
				return fmt.Errorf("a stamp names attribution %d of %d", i, len(dec.table))
			}
			s.By = dec.table[i]
		case 4:
			s.RecordedAt = time.Unix(0, int64(r.Uvarint())).UTC()
		default:
			return nil
		}
		seen[tag] = true
		return err
	})
	if err != nil {
		return Stamp{}, fmt.Errorf("a stamp: %w", err)
	}
	if seen != [5]bool{false, true, true, true, true} {
		return Stamp{}, errors.New("a stamp lacks a field")
	}
	s.Order.Writer, s.Order.ID = s.By.Checkout, s.Event
	return s, nil
}

func (dec decoder) item(b []byte) (*item, error) {
	var r item
	r.life = Live{}
	var id []byte
	var creates [][]byte
	err := wire.NewReader(b).Each(func(tag uint32, rd *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			id = rd.Bytes()
		case 2:
			creates = append(creates, rd.Bytes())
		case 3:
			var st Stamp
			st, err = dec.stamp(rd.Bytes())
			r.life = Tombstoned{By: st}
		case 4:
			r.facts, err = dec.facts(rd.Bytes())
		case 5:
			var el *element
			el, err = dec.element(rd.Bytes())
			r.elements = append(r.elements, el)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if r.id, err = events.ParseItemID(string(id)); err != nil {
		return nil, err
	}
	for _, b := range creates {
		c, err := dec.creation(r.id, b)
		if err != nil {
			return nil, err
		}
		r.creates = append(r.creates, c)
	}
	return &r, nil
}

// creation reads a create of id: its stamp and its placement, from which
// containerOf derives the container as the fold did.
func (dec decoder) creation(id events.ItemID, b []byte) (creation, error) {
	var c creation
	var place events.Placement
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			c.stamp, err = dec.stamp(r.Bytes())
		case 2:
			place, err = events.ReadPlacement(r.Bytes())
		}
		return err
	})
	switch {
	case err != nil:
		return creation{}, err
	case place == nil:
		return creation{}, errors.New("a creation has no placement")
	}
	in, ok := containerOf(id, place)
	if !ok {
		return creation{}, fmt.Errorf("a creation of %s is placed as %T, which its id does not admit", id, place)
	}
	c.in = in
	return c, nil
}

func (dec decoder) element(b []byte) (*element, error) {
	var el element
	named := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			el.name, err = events.ParseSegment(string(r.Bytes()))
			named = true
		case 2:
			var l listing
			l, err = dec.listing(r.Bytes())
			el.creates = append(el.creates, l)
		case 3:
			el.facts, err = dec.facts(r.Bytes())
		}
		return err
	})
	if err == nil && !named {
		err = errors.New("an element has no name")
	}
	return &el, err
}

func (dec decoder) listing(b []byte) (listing, error) {
	var l listing
	named := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			l.stamp, err = dec.stamp(r.Bytes())
		case 2:
			l.in, err = events.ParseName(string(r.Bytes()))
			named = true
		}
		return err
	})
	if err == nil && !named {
		err = errors.New("an element's listing has no kind name")
	}
	return l, err
}

func (dec decoder) facts(b []byte) (Facts, error) {
	var f Facts
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			reg, err := dec.register(r.Bytes())
			f.fields = append(f.fields, reg)
			return err
		case 2:
			s, err := dec.set(r.Bytes())
			f.sets = append(f.sets, s)
			return err
		}
		return nil
	})
	return f, err
}

func (dec decoder) register(b []byte) (register, error) {
	var reg register
	named := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			reg.name, err = events.ParseName(string(r.Bytes()))
			named = true
		case 2:
			var c candidate
			c, err = dec.candidate(r.Bytes())
			reg.live = append(reg.live, c)
		case 3:
			var id events.EventID
			id, err = events.ReadEventID(r.Bytes())
			reg.retired = append(reg.retired, id)
		}
		return err
	})
	if err == nil && !named {
		err = errors.New("a field has no field name")
	}
	return reg, err
}

func (dec decoder) candidate(b []byte) (candidate, error) {
	var c candidate
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			c.stamp, err = dec.stamp(r.Bytes())
		case 2:
			c.value, err = events.ReadValue(r.Bytes())
		}
		return err
	})
	if err == nil && c.value == nil {
		err = errors.New("a candidate has no value")
	}
	return c, err
}

func (dec decoder) set(b []byte) (set, error) {
	var s set
	named := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			s.name, err = events.ParseName(string(r.Bytes()))
			named = true
		case 2:
			var t tally
			t, err = dec.tally(r.Bytes())
			s.members = append(s.members, t)
		}
		return err
	})
	if err == nil && !named {
		err = errors.New("a set has no set name")
	}
	return s, err
}

func (dec decoder) tally(b []byte) (tally, error) {
	var t tally
	named := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			t.name, err = events.ParseName(string(r.Bytes()))
			named = true
		case 2:
			var st Stamp
			st, err = dec.stamp(r.Bytes())
			t.adds = append(t.adds, st)
		case 3:
			var id events.EventID
			id, err = events.ReadEventID(r.Bytes())
			t.removed = append(t.removed, id)
		}
		return err
	})
	if err == nil && !named {
		err = errors.New("a set's member has no member name")
	}
	return t, err
}

func readCarried(b []byte) (carriedAt, error) {
	var c carriedAt
	ordered := false
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			c.order, err = events.ReadOrderKey(r.Bytes())
			ordered = true
		case 2:
			c.carried.Principal, err = events.ReadPrincipalID(r.Bytes())
		case 3:
			c.carried.RecordedAt = time.Unix(0, int64(r.Uvarint())).UTC()
		case 4:
			switch n := r.Uvarint(); n {
			case reasonBadSignature:
				c.carried.Reason = BadSignature{}
			case reasonUnknownSuite:
				c.carried.Reason = UnknownSuite{}
			default:
				err = fmt.Errorf("carried for reason %d", n)
			}
		}
		return err
	})
	switch {
	case err != nil:
	case !ordered:
		err = errors.New("a carried event has no order key")
	case c.carried.Reason == nil:
		err = errors.New("a carried event has no reason")
	}
	c.carried.Event, c.carried.Writer = c.order.ID, c.order.Writer
	return c, err
}

func (dec decoder) unapplied(b []byte) (Unapplied, error) {
	var u Unapplied
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			u.Stamp, err = dec.stamp(r.Bytes())
		case 2:
			u.Kind = events.Kind(r.Bytes())
		case 3:
			u.About, err = events.ReadAbout(r.Bytes())
		case 4:
			u.Why, err = readWhy(r.Bytes())
		}
		return err
	})
	if err == nil && (u.About == nil || u.Why == nil) {
		err = errors.New("an unapplied event lacks its subject or its reason")
	}
	return u, err
}

func readWhy(b []byte) (UnappliedWhy, error) {
	var why UnappliedWhy
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case whyUnknownKind:
			r.Bytes()
			why = Unread{Why: events.UnknownKind{}}
		case whyNewerSchema:
			var n events.NewerSchema
			err := wire.NewReader(r.Bytes()).Each(func(t uint32, r *wire.Reader) error {
				switch t {
				case 1:
					n.Have = events.SchemaVersion(r.Uvarint())
				case 2:
					n.Max = events.SchemaVersion(r.Uvarint())
				}
				return nil
			})
			why = Unread{Why: n}
			return err
		case whyUndecodable:
			why = Unread{Why: events.Undecodable{Err: errors.New(string(r.Bytes()))}}
		case whyErased:
			r.Bytes()
			why = Erased{}
		case whyUncommitted:
			r.Bytes()
			why = Uncommitted{}
		case whyMisaddressed:
			why = Misaddressed{Want: string(r.Bytes())}
		}
		return nil
	})
	return why, err
}

func readMalformed(b []byte) (Malformed, error) {
	var m Malformed
	var hasRef, hasDigest bool
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			m.Ref, hasRef = string(r.Bytes()), true
		case 2:
			m.At = store.Offset(r.Uvarint())
		case 3:
			d := r.Bytes()
			if len(d) != len(m.Digest) {
				return errors.New("a malformed record's digest is not 32 bytes")
			}
			copy(m.Digest[:], d)
			hasDigest = true
		case 4:
			m.Why = string(r.Bytes())
		}
		return nil
	})
	switch {
	case err != nil:
	case !hasRef:
		err = errors.New("a malformed record has no ref")
	case !hasDigest:
		err = errors.New("a malformed record has no digest")
	}
	return m, err
}
