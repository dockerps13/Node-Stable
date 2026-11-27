package singbox

import (
	"context"
	"net"
	"testing"
	"time"
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

func TestStrictSingboxDefersStaleLocalQuotaToAuthority(t *testing.T) {
	tracker := NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"test-user": 7})
	tracker.SetDeviceLimitFunc(func(string)(int,bool){return 1,true})
	// The original snapshot local gate would reject a second distinct IP.
	first:=tracker.RoutedConnection(context.Background(),&testConn{},
		testInboundContext("test-user","192.0.2.1"),nil,nil)
	defer first.Close()
	calls:=0
	tracker.SetRevocableAdmissionFunc(func(uid int,ip string,limit int,revoke func())(func(),bool){
		calls++
		if uid!=7||ip!="192.0.2.2"||limit!=1 {t.Fatalf("unexpected strict admission: %d %s %d",uid,ip,limit)}
		return func(){},true
	})
	raw:=&testConn{}
	second:=tracker.RoutedConnection(context.Background(),raw,
		testInboundContext("test-user","192.0.2.2"),nil,nil)
	if raw.closed||calls!=1 {t.Fatalf("stale local quota pre-rejected globally authorized IP: closed=%v calls=%d",raw.closed,calls)}
	second.Close()
}

func TestStrictSingboxRevocationClosesRealLocalTCP(t *testing.T) {
	listener,err:=net.Listen("tcp","127.0.0.1:0")
	if err!=nil {t.Fatal(err)}
	defer listener.Close()
	peer,err:=net.DialTimeout("tcp",listener.Addr().String(),time.Second)
	if err!=nil {t.Fatal(err)}
	defer peer.Close()
	inbound,err:=listener.Accept()
	if err!=nil {t.Fatal(err)}
	tracker:=NewConnTracker(0)
	tracker.SetUserMap(map[string]int{"test-user":7})
	tracker.SetDeviceLimitFunc(func(string)(int,bool){return 1,true})
	var revoke func()
	tracker.SetRevocableAdmissionFunc(func(uid int,ip string,limit int,close func())(func(),bool){
		if uid!=7 || ip!="192.0.2.1" || limit!=1 {t.Fatalf("unexpected identity: %d %s %d",uid,ip,limit)}
		revoke=close
		return func(){},true
	})
	tracked:=tracker.RoutedConnection(context.Background(),inbound,
		testInboundContext("test-user","192.0.2.1"),nil,nil)
	defer tracked.Close()
	if revoke==nil {t.Fatal("strict remote lease hook not installed")}
	_,err=peer.Write([]byte("first byte"))
	if err!=nil {t.Fatal(err)}
	revoke()
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	_,err=peer.Read(one[:])
	if err==nil {t.Fatal("peer socket remained open after lease revoked")}
	if timeout,ok:=err.(net.Error);ok&&timeout.Timeout() {
		t.Fatalf("revoke did not close the TCP socket before deadline: %v",err)
	}
	if tracker.ActiveCount()!=0 {t.Fatalf("live connections after remote revoke: %d",tracker.ActiveCount())}
}
