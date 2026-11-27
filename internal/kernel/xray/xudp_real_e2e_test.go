//go:build xray_xudp_e2e

package xray

import (
    "bytes"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "sync/atomic"
    "testing"
    "time"

    "github.com/cedar2025/xboard-node/internal/config"
    "github.com/cedar2025/xboard-node/internal/kernel"
    "github.com/cedar2025/xboard-node/internal/model"
    xrayCore "github.com/xtls/xray-core/core"
    "github.com/xtls/xray-core/infra/conf/serial"
)

func xraySocksUDPAssociate(t *testing.T, port int) (*net.UDPConn, net.Conn) {
    t.Helper()
    ctrl, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d",port), 3*time.Second)
    if err != nil { t.Fatal(err) }
    t.Cleanup(func(){ _ = ctrl.Close() })
    _ = ctrl.SetDeadline(time.Now().Add(5*time.Second))
    if _, err := ctrl.Write([]byte{5,1,0}); err != nil { t.Fatal(err) }
    greeting := make([]byte, 2)
    if _, err := io.ReadFull(ctrl,greeting); err != nil || !bytes.Equal(greeting,[]byte{5,0}) {
        t.Fatalf("SOCKS greeting %v: %v",greeting,err)
    }
    if _, err := ctrl.Write([]byte{5,3,0,1,0,0,0,0,0,0}); err != nil { t.Fatal(err) }
    head := make([]byte,4)
    if _, err := io.ReadFull(ctrl,head); err != nil { t.Fatal(err) }
    if head[0]!=5 || head[1]!=0 { t.Fatalf("SOCKS UDP associate reply %v",head) }
    var ip net.IP
    switch head[3] {
    case 1:
        raw:=make([]byte,4)
        if _,err:=io.ReadFull(ctrl,raw);err!=nil {t.Fatal(err)}
        ip=net.IP(raw)
    case 4:
        raw:=make([]byte,16)
        if _,err:=io.ReadFull(ctrl,raw);err!=nil {t.Fatal(err)}
        ip=net.IP(raw)
    default:
        t.Fatalf("unexpected SOCKS UDP bound address type %d",head[3])
    }
    two:=make([]byte,2)
    if _,err:=io.ReadFull(ctrl,two);err!=nil {t.Fatal(err)}
    boundPort:=int(binary.BigEndian.Uint16(two))
    if ip.IsUnspecified() { ip=net.IPv4(127,0,0,1) }
    if !ip.IsLoopback() {t.Fatalf("SOCKS returned non-loopback UDP relay %v",ip)}
    udp,err:=net.DialUDP("udp",nil,&net.UDPAddr{IP:ip,Port:boundPort})
    if err!=nil {t.Fatal(err)}
    t.Cleanup(func(){_ = udp.Close()})
    _ = ctrl.SetDeadline(time.Time{})
    return udp,ctrl
}

func TestRealXrayXUDPRoundTripAndRevocation(t *testing.T) {
    const uuid="aaaaaaaa-1111-2222-3333-444444444444"
    target,err:=net.ListenPacket("udp","127.0.0.1:0")
    if err!=nil { t.Fatal(err) }
    defer target.Close()
    finished:=make(chan struct{})
    go func(){
        defer close(finished)
        buffer:=make([]byte,4096)
        for {
            n,addr,err:=target.ReadFrom(buffer)
            if err!=nil { return }
            _,_ = target.WriteTo(buffer[:n],addr)
        }
    }()
    defer func(){ _ = target.Close(); <-finished }()

    serverPort:=reserveXrayLoopbackPort(t)
    clientPort:=reserveXrayLoopbackPort(t)
    callbacks:=make(chan func(),32)
    var allow atomic.Bool
    allow.Store(true)
    var grants atomic.Int64
    server:=New(config.KernelConfig{Type:"xray",LogLevel:"error",CustomRoute:[]map[string]any{
        {"type":"field","ip":[]string{"127.0.0.1/32"},"outboundTag":"direct"},
    }})
    server.SetRevocableAdmissionFunc(func(uid int, ip string, limit int, revoke func()) (func(),bool){
        if uid!=7 || ip!="127.0.0.1" || limit!=1 || revoke==nil || !allow.Load() {
            return nil,false
        }
        grants.Add(1)
        select{case callbacks<-revoke:default:}
        return func(){},true
    })
    if err:=server.Start(&model.NodeSpec{Protocol:"vless",ServerPort:serverPort,ListenIP:"127.0.0.1"},
        []model.UserSpec{{ID:7,UUID:uuid,DeviceLimit:1}},kernel.TLSCert{});err!=nil {t.Fatal(err)}
    defer server.Stop()

    configJSON:=M{
        "log":M{"loglevel":"error"},
        "inbounds":[]M{{"listen":"127.0.0.1","port":clientPort,"protocol":"socks","settings":M{
            "auth":"noauth","udp":true,"ip":"127.0.0.1",
        }}},
        "outbounds":[]M{{"protocol":"vless","tag":"remote",
            "settings":M{"vnext":[]M{{"address":"127.0.0.1","port":serverPort,
                "users":[]M{{"id":uuid,"encryption":"none"}}}}},
            "streamSettings":M{"network":"tcp"},
            "mux":M{"enabled":true,"concurrency":4,"xudpConcurrency":4,"xudpProxyUDP443":"allow"},
        }},
    }
    raw,err:=json.Marshal(configJSON)
    if err!=nil {t.Fatal(err)}
    pb,err:=serial.LoadJSONConfig(bytes.NewReader(raw))
    if err!=nil {t.Fatal(err)}
    client,err:=xrayCore.New(pb)
    if err!=nil {t.Fatal(err)}
    defer client.Close()
    if err:=client.Start();err!=nil {t.Fatal(err)}

    udp,_:=xraySocksUDPAssociate(t,clientPort)
    dest:=target.LocalAddr().(*net.UDPAddr)
    header:=[]byte{0,0,0,1,127,0,0,1,0,0}
    binary.BigEndian.PutUint16(header[8:10],uint16(dest.Port))
    recv:=make([]byte,4096)
    for i:=0;i<3;i++{
        payload:=[]byte(fmt.Sprintf("xudp-live-%d",i))
        if _,err:=udp.Write(append(append([]byte{},header...),payload...));err!=nil {t.Fatal(err)}
        _ = udp.SetReadDeadline(time.Now().Add(5*time.Second))
        n,err:=udp.Read(recv)
        if err!=nil {t.Fatalf("XUDP packet #%d roundtrip: %v",i,err)}
        if n<10 || !bytes.Equal(recv[n-len(payload):n],payload){
            t.Fatalf("XUDP packet #%d unexpected payload %q",i,recv[:n])
        }
    }
    if grants.Load()==0 { t.Fatal("XUDP traffic bypassed strict admission callback") }
    // Authority loss must deny new leases as well as revoke active associations.
    allow.Store(false)
    revoked:=0
drain:
    for{
        select {
        case cb:=<-callbacks: cb(); revoked++
        default: break drain
        }
    }
    if revoked==0 {t.Fatal("no XUDP lease callbacks available for revocation")}
    time.Sleep(150*time.Millisecond)
    if _,err:=udp.Write(append(append([]byte{},header...),[]byte("xudp-after-revoke")...));err!=nil {
        t.Fatalf("XUDP write after revocation: %v",err)
    }
    _=udp.SetReadDeadline(time.Now().Add(750*time.Millisecond))
    n,err:=udp.Read(recv)
    if err==nil { t.Fatalf("revoked XUDP still reached relay, packet=%q",recv[:n]) }
    var netErr net.Error
    if !errors.As(err,&netErr) || !netErr.Timeout() {
        t.Fatalf("post-revoke XUDP read unexpectedly failed: %v",err)
    }
}
