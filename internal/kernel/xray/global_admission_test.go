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
