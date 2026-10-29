package portable

import "testing"

func TestQuotaReserveGrowRollbackAndRelease(t *testing.T) {
	var q winOptQuota
	var a, b uint64
	if !q.reserve(&a, 1<<20, 2<<20) || a == 0 {
		t.Fatal("first reserve failed")
	}
	if !q.reserve(&b, 1<<20, 2<<20) || b == 0 || b == a {
		t.Fatal("second reserve failed")
	}
	if q.reserve(&a, 2<<20, 2<<20) {
		t.Fatal("over budget accepted")
	}
	if q.reserved(a) != 1<<20 || q.used != 2<<20 {
		t.Fatal("failed grow changed accounting")
	}
	q.release(b)
	if !q.reserve(&a, 2<<20, 2<<20) {
		t.Fatal("growth after release failed")
	}
	if !q.reserve(&a, 1<<20, 2<<20) {
		t.Fatal("rollback failed")
	}
	q.release(a)
	q.release(a)
	if q.used != 0 {
		t.Fatal("leaked quota")
	}
}
