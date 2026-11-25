package globaladmission

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cedar2025/xboard-node/internal/config"
	"github.com/cedar2025/xboard-node/internal/panel"
)

// Test helper executes in an independent OS process and uses the real HTTP
// Node-Stable client and lease manager. It never connects to production.
func TestGlobalAdmissionProcessWorker(t *testing.T) {
	if os.Getenv("GLOBAL_ADMISSION_WORKER") != "1" {return}
	machine,_:=strconv.Atoi(os.Getenv("TEST_MACHINE"))
	node,_:=strconv.Atoi(os.Getenv("TEST_NODE"))
	ip:=os.Getenv("TEST_IP")
	client:=panel.NewClient(config.PanelConfig{
		URL:os.Getenv("TEST_URL"),Token:os.Getenv("TEST_TOKEN"),MachineID:machine,
	}).ForNode(node)
	manager:=NewManager(context.Background(),client)
	manager.tick=time.Hour
	release,ok:=manager.Acquire(7,ip,2,func(){})
	if !ok {
		fmt.Println("DENIED")
		return
	}
	fmt.Println("GRANTED")
	_,_=bufio.NewReader(os.Stdin).ReadString('\n')
	release()
	drainCtx,drainCancel:=context.WithTimeout(context.Background(),3*time.Second)
	defer drainCancel()
	if err:=manager.WaitReleases(drainCtx);err!=nil {t.Fatal(err)}
}

// This test runs a shared Redis authority + real HTTP requests from two
// separate OS processes. The HTTP service is still a simulated XBoard handler.
func TestTwoIndependentNodeProcesses(t *testing.T) {
	if os.Getenv("GLOBAL_ADMISSION_REDIS_INTEGRATION")!="1" {t.Skip("isolated Redis integration only")}
	script,err:=filepath.Abs("redis_lease.lua")
	if err!=nil {t.Fatal(err)}
	key:=fmt.Sprintf("ga-process:%d:user:7",time.Now().UnixNano())
	defer exec.Command("redis-cli","-h","127.0.0.1","-p","6379","DEL",key).Run()
	eval:=func(action,owner,ip string)([]string,error){
		ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second)
		defer cancel()
		out,err:=exec.CommandContext(ctx,"redis-cli","--raw","-h","127.0.0.1","-p","6379",
			"--eval",script,key,",",action,owner,ip,"2","30000").CombinedOutput()
		if err!=nil {return nil,fmt.Errorf("redis: %w: %s",err,out)}
		values:=strings.Fields(string(out))
		if len(values)!=3 {return nil,fmt.Errorf("bad reply: %s",out)}
		return values,nil
	}
	server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
		var in struct{
			Token string `json:"token"`
			Machine int `json:"machine_id"`
			Node int `json:"node_id"`
			Action string `json:"action"`
			User int `json:"user_id"`
			ID string `json:"lease_id"`
			IP string `json:"ip"`
		}
		if r.Method!="POST"||r.URL.Path!="/api/v2/server/device-lease" ||
			json.NewDecoder(http.MaxBytesReader(w,r.Body,4096)).Decode(&in)!=nil {
			http.Error(w,"invalid request",400);return
		}
		if in.User!=7 || !((in.Machine==101&&in.Node==11&&in.Token=="secret-a")||
			(in.Machine==202&&in.Node==22&&in.Token=="secret-b")) {
			http.Error(w,"forbidden",403);return
		}
		values,err:=eval(in.Action,fmt.Sprintf("m%d:n%d:%s",in.Machine,in.Node,in.ID),in.IP)
		if err!=nil {http.Error(w,"Redis unavailable",503);return}
		allowed,_:=strconv.Atoi(values[0])
		count,_:=strconv.Atoi(values[1])
		ttl,_:=strconv.Atoi(values[2])
		w.Header().Set("Content-Type","application/json")
		_ = json.NewEncoder(w).Encode(panel.DeviceLeaseReply{Allowed:allowed==1,DistinctIPs:count,LeaseTTLMS:ttl})
	}))
	defer server.Close()

	type worker struct{
		cmd *exec.Cmd
		stdin io.WriteCloser
		stdout *bufio.Reader
	}
	start:=func(machine,node int,token,ip string)*worker{
		t.Helper()
		cmd:=exec.Command(os.Args[0],"-test.run=^TestGlobalAdmissionProcessWorker$","-test.v")
		cmd.Env=append(os.Environ(),
			"GLOBAL_ADMISSION_WORKER=1","TEST_URL="+server.URL,
			"TEST_MACHINE="+strconv.Itoa(machine),"TEST_NODE="+strconv.Itoa(node),
			"TEST_TOKEN="+token,"TEST_IP="+ip)
		stdin,err:=cmd.StdinPipe()
		if err!=nil {t.Fatal(err)}
		stdout,err:=cmd.StdoutPipe()
		if err!=nil {t.Fatal(err)}
		if err=cmd.Start();err!=nil {t.Fatal(err)}
		return &worker{cmd:cmd,stdin:stdin,stdout:bufio.NewReader(stdout)}
	}
	// -test.v prints the Go test header. Read up to a bounded number of
	// lines to locate the worker's explicit admission result.
	result:=func(w *worker)string{
		t.Helper()
		answer:=make(chan string,1)
		go func(){
			for i:=0;i<12;i++ {
				line,err:=w.stdout.ReadString('\n')
				if err!=nil {answer<-"EOF";return}
				line=strings.TrimSpace(line)
				if line=="GRANTED"||line=="DENIED" {answer<-line;return}
			}
			answer<-"MISSING"
		}()
		select{
		case val:=<-answer:return val
		case <-time.After(8*time.Second):t.Fatal("worker did not respond");return ""
		}
	}
	stop:=func(w *worker){
		t.Helper()
		_,_=io.WriteString(w.stdin,"stop\n")
		_ = w.stdin.Close()
		if err:=w.cmd.Wait();err!=nil {t.Errorf("worker exit: %v",err)}
	}
	a:=start(101,11,"secret-a","192.0.2.1")
	if v:=result(a);v!="GRANTED" {t.Fatalf("node A: %s",v)}
	defer func(){if a!=nil {stop(a)}}()
	b:=start(202,22,"secret-b","192.0.2.2")
	if v:=result(b);v!="GRANTED" {t.Fatalf("node B: %s",v)}
	defer func(){if b!=nil {stop(b)}}()
	c:=start(202,22,"secret-b","192.0.2.3")
	if v:=result(c);v!="DENIED" {t.Fatalf("third IP: %s",v)}
	stop(c)
	shared:=start(202,22,"secret-b","192.0.2.1")
	if v:=result(shared);v!="GRANTED" {t.Fatalf("shared IP: %s",v)}
	stop(shared)
	stop(a)
	a=nil
	// Node B still holds second IP; after releasing A and shared owner,
	// a third IP should now have one free distinct-IP slot.
	deadline:=time.Now().Add(3*time.Second)
	for{
		values,err:=eval("count","probe","")
		if err!=nil {t.Fatal(err)}
		if values[1]=="1" {break}
		if time.Now().After(deadline) {t.Fatalf("lease release not observed: %v",values)}
		time.Sleep(20*time.Millisecond)
	}
	d:=start(101,11,"secret-a","192.0.2.3")
	if v:=result(d);v!="GRANTED" {t.Fatalf("released slot not reused: %s",v)}
	stop(d)
	stop(b)
	b=nil
}
