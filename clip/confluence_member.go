package clip

// coincidentMembership computes the op-membership of the region just BELOW and
// just ABOVE a coincident cross-source horizontal pair (e1,e2), evaluated at the
// overlap's x-midpoint via a fresh signed-winding scan of the AEL's
// non-horizontal edges. The signed sum uses each edge's WindDx, so a hole (a
// CW inner ring with opposite WindDx) correctly cancels the enclosing outer
// winding — the count is hole-aware. Returns (belowMember, aboveMember).
func coincidentMembership(ael *AEL, op Operation, e1, e2 *ActiveEdge) (bool, bool) {
	y := e1.Seg.Bot.Y
	xa := max(e1.Seg.Bot.X, e2.Seg.Bot.X)
	xb := min(e1.Seg.Top.X, e2.Seg.Top.X)
	xm := (xa + xb) / 2

	var bs, bc, as, ac int // below/above subject/clip winding
	for i := range ael.Len() {
		e := ael.At(i)
		if e.Seg.Horizontal() || XAtY(e.Seg, y) >= xm {
			continue
		}
		below := e.Seg.Bot.Y < y && e.Seg.Top.Y >= y
		above := e.Seg.Bot.Y <= y && e.Seg.Top.Y > y
		switch e.Seg.Src {
		case Subject:
			if below {
				bs += e.WindDx
			}
			if above {
				as += e.WindDx
			}
		default:
			if below {
				bc += e.WindDx
			}
			if above {
				ac += e.WindDx
			}
		}
	}
	return opMember(bs, bc, op), opMember(as, ac, op)
}
