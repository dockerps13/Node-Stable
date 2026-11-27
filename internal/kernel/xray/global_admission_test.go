package xray

import "testing"

func TestRevocableXrayAdmissionPrefersStrictAuthority(t *testing.T) {
	d := newTestDispatcher()
	d.UpdateLimits(map[string]int{"user@7":7}, map[string]int{"user@7":1}, nil)
	d.SetAdmissionFunc(func(int,string,int)(func(),bool){
		t.Fatal("legacy callback used with strict global admission")
		return nil,false
	})
	var revoke func()
	called := 0
	d.SetRevocableAdmissionFunc(func(uid int,ip string,limit int, close func())(func(),bool){
		if uid!=7 || ip!="192.0.2.1" || limit!=1 || close==nil { t.Fatalf("bad strict args %d %s %d",uid,ip,limit) }
		revoke=close
		return func(){called++},true
	})
	release,ok:=d.reserveMachineSlot("user@7","192.0.2.1",func(){called++})
	if !ok || release==nil || revoke==nil {t.Fatal("global authority not installed")}
	revoke()
	release()
	if called!=2 {t.Fatalf("revoke/release called %d times",called)}
}

func TestStrictXrayDoesNotRejectFromStaleLocalSnapshot(t *testing.T) {
	d := newTestDispatcher()
	d.UpdateLimits(map[string]int{"user@7": 7}, map[string]int{"user@7": 1}, nil)
	// Under strict global mode, these are observations, not admission grants.
	d.mu.Lock()
	d.limitedIPs["user@7"] = map[string]int{"192.0.2.1": 1}
	d.mu.Unlock()
	d.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, close func()) (func(), bool) {
		if uid != 7 || ip != "192.0.2.2" || limit != 1 {
			t.Fatalf("unexpected authority args: %d %s %d", uid, ip, limit)
		}
		return func(){}, true
	})
	if denied := d.checkDeviceLimit("user@7", "192.0.2.2", true); denied {
		t.Fatal("local stale IP rejected before remote lease authority could decide")
	}
	release, allowed := d.reserveMachineSlot("user@7", "192.0.2.2", func(){})
	if !allowed || release == nil {t.Fatal("global authority allowed IP but connection rejected")}
	release()
	d.delConn("user@7","192.0.2.2")
	if count:=d.limitedIPs["user@7"]["192.0.2.2"];count!=0 {
		t.Fatalf("connection accounting leaked %d",count)
	}
}
