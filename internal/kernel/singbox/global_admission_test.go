package singbox

import (
	"context"
	"testing"
)

func TestRevocableSingboxAdmissionClosesTCP(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"test-user":7})
	tracker.SetDeviceLimitFunc(func(string)(int,bool){return 1,true})
	var revoke func()
	releases := 0
	tracker.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, close func())(func(),bool){
		if uid!=7||ip!="192.0.2.1"||limit!=1 {t.Fatalf("bad admission %d %s %d",uid,ip,limit)}
		revoke=close
		return func(){releases++},true
	})
	raw := &testConn{}
	tracker.RoutedConnection(context.Background(),raw,testInboundContext("test-user","192.0.2.1"),nil,nil)
	if raw.closed||revoke==nil {t.Fatal("not admitted")}
	revoke()
	if !raw.closed {t.Fatal("lease revoked but TCP connection stayed open")}
	if releases!=1||tracker.ActiveCount()!=0 {t.Fatalf("release=%d active=%d",releases,tracker.ActiveCount())}
	revoke()
	if releases!=1 {t.Fatal("duplicate release")}
}
