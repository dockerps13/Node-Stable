package globaladmission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/panel"
)

// This is a fully isolated *HTTP + real Redis* integration test. The HTTP
// handler is a test double for the XBoard PHP endpoint, NOT XBoard itself.
// Two separate node clients with distinct machine credentials share the
// same Redis account authority. A real PHP and two-process E2E test remains
// a release requirement.
func TestTwoMachineClientsHTTPRedisAuthority(t *testing.T) {
	if os.Getenv("GLOBAL_ADMISSION_REDIS_INTEGRATION") != "1" {
		t.Skip("enable with GLOBAL_ADMISSION_REDIS_INTEGRATION=1 and local disposable redis")
	}
	lua, err := filepath.Abs("redis_lease.lua")
	if err != nil { t.Fatal(err) }
	key := fmt.Sprintf("ga-http-ci:%d:user:7", time.Now().UnixNano())
	eval := func(action, owner, ip string, limit, ttl int) ([]string, error) {
		args := []string{"--raw", "-h", "127.0.0.1", "-p", "6379", "--eval", lua,
			key, ",", action, owner, ip, strconv.Itoa(limit), strconv.Itoa(ttl)}
		out, err := exec.Command("redis-cli", args...).CombinedOutput()
		if err != nil {return nil,fmt.Errorf("redis-cli: %w: %s",err,out)}
		lines := strings.Fields(strings.TrimSpace(string(out)))
		if len(lines)!=3 {return nil,fmt.Errorf("invalid redis reply: %s",out)}
		return lines,nil
	}
	defer exec.Command("redis-cli","-h","127.0.0.1","-p","6379","DEL",key).Run()

	var unavailable atomic.Bool
	var shortTTL atomic.Bool
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		if r.URL.Path!="/api/v2/server/device-lease" || r.Method!="POST" {
			http.NotFound(w,r);return
		}
		var payload struct {
			Token string `json:"token"`
			MachineID int `json:"machine_id"`
			NodeID int `json:"node_id"`
			Action string `json:"action"`
			UserID int `json:"user_id"`
			LeaseID string `json:"lease_id"`
			IP string `json:"ip"`
		}
		if json.NewDecoder(http.MaxBytesReader(w,r.Body,4096)).Decode(&payload)!=nil {
			http.Error(w,"bad JSON",400);return
		}
		authorized := (payload.MachineID==101 && payload.NodeID==11 && payload.Token=="secret-a") ||
			(payload.MachineID==202 && payload.NodeID==22 && payload.Token=="secret-b")
		if !authorized || payload.UserID!=7 {
			http.Error(w,"forbidden",403);return
		}
		if unavailable.Load() {http.Error(w,"unavailable",503);return}
		// Distinct machine+node owner identity is derived server side, never
		// supplied by the client itself.
		owner:=fmt.Sprintf("m%d:n%d:%s",payload.MachineID,payload.NodeID,payload.LeaseID)
		ttl:=30000
		if shortTTL.Load() {ttl=180}
		requests.Add(1)
		values,err:=eval(payload.Action,owner,payload.IP,2,ttl)
		if err!=nil {http.Error(w,"authority failure",503);return}
		granted,_:=strconv.Atoi(values[0])
		distinct,_:=strconv.Atoi(values[1])
		leaseTTL,_:=strconv.Atoi(values[2])
		w.Header().Set("Content-Type","application/json")
		_ = json.NewEncoder(w).Encode(panel.DeviceLeaseReply{Allowed:granted==1,DistinctIPs:distinct,LeaseTTLMS:leaseTTL})
	}))
	defer server.Close()

	clientA:=panel.NewClient(config.PanelConfig{URL:server.URL,Token:"secret-a",MachineID:101}).ForNode(11)
	clientB:=panel.NewClient(config.PanelConfig{URL:server.URL,Token:"secret-b",MachineID:202}).ForNode(22)
	ctx,cancel:=context.WithCancel(context.Background())
	defer cancel()
	a:=NewManager(ctx,clientA)
	b:=NewManager(ctx,clientB)
	a.tick=40*time.Millisecond
	b.tick=40*time.Millisecond
	var revoked atomic.Int64
	lose:=func(){revoked.Add(1)}
	release1,ok:=a.Acquire(7,"192.0.2.1",2,lose)
	if !ok {t.Fatal("machine A first IP denied")}
	defer release1()
	release2,ok:=b.Acquire(7,"192.0.2.2",2,lose)
	if !ok {t.Fatal("machine B second IP denied")}
	defer release2()
	if release,ok:=b.Acquire(7,"192.0.2.3",2,lose);ok {
		release();t.Fatal("third distinct IP allowed across two machines")
	}
	shared,ok:=b.Acquire(7,"::ffff:192.0.2.1",2,lose)
	if !ok {t.Fatal("machine B same canonical IP rejected")}
	defer shared()
	// Release machine A owner; machine B still owns the shared IP.
	release1()
	if release,ok:=a.Acquire(7,"192.0.2.3",2,lose);ok {
		release();t.Fatal("machine A released machine B's shared slot")
	}
	shared()
	release2()
	// Async release is bounded and must be observed before retry.
	deadline:=time.Now().Add(2*time.Second)
	for {
		result,err:=eval("count","probe","",2,30000)
		if err!=nil {t.Fatal(err)}
		if result[1]=="0" {break}
		if time.Now().After(deadline) {t.Fatalf("last release did not free slot: %v",result)}
		time.Sleep(15*time.Millisecond)
	}
	third,ok:=a.Acquire(7,"192.0.2.3",2,lose)
	if !ok {t.Fatal("slot not recoverable after final release")}
	third()

	// Panel outage must reject new acquisitions, including a different IP.
	unavailable.Store(true)
	if release,ok:=b.Acquire(7,"192.0.2.4",2,lose);ok {
		release();t.Fatal("outage granted a new device")
	}
	unavailable.Store(false)

	// Confirmed short leases must not remain active indefinitely when the
	// panel goes away; a manager's revoker must be called.
	shortTTL.Store(true)
	revokedBefore:=revoked.Load()
	short,ok:=b.Acquire(7,"192.0.2.5",2,lose)
	if !ok {t.Fatal("short lease denied")}
	defer short()
	unavailable.Store(true)
	waitUntil:=time.Now().Add(2*time.Second)
	for revoked.Load()==revokedBefore && time.Now().Before(waitUntil) {
		time.Sleep(15*time.Millisecond)
	}
	if revoked.Load()==revokedBefore {t.Fatal("expired lease did not revoke connection under panel outage")}
	if requests.Load()<4 {t.Fatal("expected real HTTP requests to Redis-backed authority")}
}
