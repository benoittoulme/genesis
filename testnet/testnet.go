//Package testnet helps to manage and control current testnets
package testnet

import (
	"../db"
	"../ssh"
	"../state"
	"../status"
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// TestNet represents a testnet and some state on that testnet. Should also contain the details needed to
// rebuild tn testnet.
type TestNet struct {
	TestNetID string
	//Servers contains the servers on which the testnet resides
	Servers []db.Server
	//Nodes contains the active nodes in the network, including the newly built nodes
	Nodes []db.Node
	//NewlyBuiltNodes contains only the nodes created in the last/in progress build event
	NewlyBuiltNodes []db.Node
	//Clients is a map of server ids to ssh clients
	Clients map[int]*ssh.Client
	//BuildState is the build state for the test net
	BuildState *state.BuildState
	//Details contains all of the past deployments to tn test net
	Details []db.DeploymentDetails
	//CombinedDetails contains all of the deployments merged into one
	CombinedDetails db.DeploymentDetails
	//LDD is a pointer to latest deployment details
	LDD *db.DeploymentDetails
	mux *sync.RWMutex
}

//RestoreTestNet fetches a testnet which already exists.
func RestoreTestNet(buildID string) (*TestNet, error) {
	out := new(TestNet)
	err := db.GetMetaP("testnet_"+buildID, out)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	bs, err := state.GetBuildStateByID(buildID)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	out.BuildState = bs
	out.mux = &sync.RWMutex{}
	out.LDD = out.GetLastestDeploymentDetails()

	out.Clients = map[int]*ssh.Client{}
	for _, server := range out.Servers {
		out.Clients[server.ID], err = status.GetClient(server.ID)
		if err != nil {
			log.Println(err)
			out.BuildState.ReportError(err)
			return nil, err
		}
	}
	return out, nil
}

// NewTestNet creates a new TestNet
func NewTestNet(details db.DeploymentDetails, buildID string) (*TestNet, error) {
	var err error
	out := new(TestNet)

	out.TestNetID = buildID
	out.Nodes = []db.Node{}
	out.NewlyBuiltNodes = []db.Node{}
	out.Details = []db.DeploymentDetails{details}
	out.CombinedDetails = details
	out.LDD = &details
	out.mux = &sync.RWMutex{}

	out.BuildState, err = state.GetBuildStateByID(buildID)
	if err != nil {
		log.Println(err)
		return nil, err
	}

	// FETCH THE SERVERS
	out.Servers, err = db.GetServers(details.Servers)
	if err != nil {
		log.Println(err)
		out.BuildState.ReportError(err)
		return nil, err
	}
	fmt.Println("Got the Servers")

	//OPEN UP THE RELEVANT SSH CONNECTIONS
	out.Clients = map[int]*ssh.Client{}

	for _, server := range out.Servers {
		out.Clients[server.ID], err = status.GetClient(server.ID)
		if err != nil {
			log.Println(err)
			out.BuildState.ReportError(err)
			return nil, err
		}
	}
	return out, nil
}

//AddNode adds a node to the testnet and returns the new nodes absolute number
func (tn *TestNet) AddNode(node db.Node) int {
	tn.mux.Lock()
	defer tn.mux.Unlock()
	node.AbsoluteNum = len(tn.Nodes)
	tn.NewlyBuiltNodes = append(tn.NewlyBuiltNodes, node)
	tn.Nodes = append(tn.Nodes, node)
	return node.AbsoluteNum
}

// AddDetails adds the details of a new deployment to the TestNet
func (tn *TestNet) AddDetails(dd db.DeploymentDetails) error {
	tn.mux.Lock()
	defer tn.mux.Unlock()
	tn.Details = append(tn.Details, dd)
	//MERGE
	tmp, err := json.Marshal(dd)
	if err != nil {
		log.Println(err)
		return err
	}
	tn.LDD = &tn.Details[len(tn.Details)-1]

	oldCD := tn.CombinedDetails
	err = json.Unmarshal(tmp, &tn.CombinedDetails)
	if err != nil {
		log.Println(err)
	}

	/**Handle Files**/
	tn.CombinedDetails.Files = oldCD.Files
	if dd.Files != nil && len(dd.Files) > 0 {
		if tn.CombinedDetails.Files == nil {
			tn.CombinedDetails.Files = make([]map[string]string, oldCD.Nodes)
		}
		if len(tn.CombinedDetails.Files) < oldCD.Nodes {
			for i := len(tn.CombinedDetails.Files); i < oldCD.Nodes; i++ {
				tn.CombinedDetails.Files = append(tn.CombinedDetails.Files, map[string]string{})
			}
		}
		for _, files := range dd.Files {
			tn.CombinedDetails.Files = append(tn.CombinedDetails.Files, files)
		}
	}

	/**Handle Nodes**/
	tn.CombinedDetails.Nodes = oldCD.Nodes + dd.Nodes

	/**Handle Images***/
	if dd.Images != nil && len(dd.Images) > 0 {
		if tn.CombinedDetails.Images == nil {
			tn.CombinedDetails.Images = make([]string, oldCD.Nodes)
		}
		if len(tn.CombinedDetails.Images) < oldCD.Nodes {
			for i := len(tn.CombinedDetails.Images); i < oldCD.Nodes; i++ {
				tn.CombinedDetails.Images = append(tn.CombinedDetails.Images, tn.CombinedDetails.Images[0])
			}
		}
		for _, image := range dd.Images {
			tn.CombinedDetails.Images = append(tn.CombinedDetails.Images, image)
		}
	}
	return nil
}

// FinishedBuilding empties the NewlyBuiltNodes, signals DoneBuilding on the BuildState, and
// stores the current data of tn testnet
func (tn *TestNet) FinishedBuilding() {
	tn.BuildState.DoneBuilding()
	tn.NewlyBuiltNodes = []db.Node{}
	tn.Store()
}

// GetFlatClients takes the clients map and turns it into an array
// for easy iterator
func (tn *TestNet) GetFlatClients() []*ssh.Client {
	out := []*ssh.Client{}
	tn.mux.RLock()
	defer tn.mux.RUnlock()
	for _, client := range tn.Clients {
		out = append(out, client)
	}
	return out
}

//GetServer retrieves a server the TestNet resides on by id
func (tn *TestNet) GetServer(id int) *db.Server {
	for _, server := range tn.Servers {
		if server.ID == id {
			return &server
		}
	}
	return nil
}

//GetLastestDeploymentDetails gets a pointer to the latest deployment details
func (tn *TestNet) GetLastestDeploymentDetails() *db.DeploymentDetails {
	tn.mux.RLock()
	defer tn.mux.RUnlock()
	return &tn.Details[len(tn.Details)-1]
}

//PreOrderNodes sorts the nodes into buckets by server id
func (tn *TestNet) PreOrderNodes() map[int][]db.Node {
	tn.mux.RLock()
	defer tn.mux.RUnlock()

	out := make(map[int][]db.Node)
	for _, server := range tn.Servers {
		out[server.ID] = []db.Node{}
	}

	for _, node := range tn.Nodes {
		out[node.Server] = append(out[node.Server], node)
	}
	return out
}

//PreOrderNewNodes sorts the newly built nodes into buckets by server id
func (tn *TestNet) PreOrderNewNodes() map[int][]db.Node {
	tn.mux.RLock()
	defer tn.mux.RUnlock()

	out := make(map[int][]db.Node)
	for _, server := range tn.Servers {
		out[server.ID] = []db.Node{}
	}

	for _, node := range tn.NewlyBuiltNodes {
		out[node.Server] = append(out[node.Server], node)
	}
	return out
}

//Store stores the TestNets data for later retrieval
func (tn *TestNet) Store() {
	db.SetMeta("testnet_"+tn.TestNetID, *tn)
}

//Destroy removes all the testnets data
func (tn *TestNet) Destroy() error {
	return db.DeleteMeta("testnet_" + tn.TestNetID)
}

//StoreNodes stores the newly built nodes into the database with their labels.
func (tn *TestNet) StoreNodes(labels []string) error {
	for i, node := range tn.NewlyBuiltNodes {
		if labels != nil {
			node.Label = labels[i]
		}

		_, err := db.InsertNode(node)
		if err != nil {
			log.Println(err)
		}
	}
	return nil
}