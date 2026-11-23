package machine

import (
    "testing"

    "github.com/cedar2025/xboard-node/internal/controlplane"
)

func TestUnregisterNodeDoesNotDeleteReplacementGeneration(t *testing.T) {
    const nodeID=25
    oldMailbox:=controlplane.NewNodeMailbox()
    replacementMailbox:=controlplane.NewNodeMailbox()
    oldStatus:=make(chan controlplane.StatusChange,1)
    newStatus:=make(chan controlplane.StatusChange,1)
    o:= &Orchestrator{
        mailboxes: map[int]*controlplane.NodeMailbox{nodeID:oldMailbox},
        statuses: map[int]chan<- controlplane.StatusChange{nodeID:oldStatus},
    }
    // A replacement can register before the old generation returns from Run.
    o.mailboxes[nodeID]=replacementMailbox
    o.statuses[nodeID]=newStatus
    o.unregisterNode(nodeID,oldMailbox)
    if o.mailboxes[nodeID]!=replacementMailbox {
        t.Fatal("old service deleted new generation's mailbox")
    }
    if o.statuses[nodeID]!=newStatus {
        t.Fatal("old service deleted new generation's WS status subscriber")
    }
    o.unregisterNode(nodeID,replacementMailbox)
    if _,ok:=o.mailboxes[nodeID];ok {t.Fatal("current generation mailbox still registered")}
    if _,ok:=o.statuses[nodeID];ok {t.Fatal("current generation status subscriber still registered")}
}

func TestUnregisterNodePreservesUnrelatedSubscribers(t *testing.T) {
    o:= &Orchestrator{
        mailboxes:map[int]*controlplane.NodeMailbox{
            18:controlplane.NewNodeMailbox(),25:controlplane.NewNodeMailbox(),
        },
        statuses:map[int]chan<- controlplane.StatusChange{
            18:make(chan controlplane.StatusChange),25:make(chan controlplane.StatusChange),
        },
    }
    old:=o.mailboxes[25]
    o.unregisterNode(25,old)
    if o.mailboxes[18]==nil || o.statuses[18]==nil {
        t.Fatal("unregister changed a different node's resources")
    }
}
