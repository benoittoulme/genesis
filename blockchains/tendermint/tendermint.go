package tendermint

import (
	db "../../db"
	ssh "../../ssh"
	state "../../state"
	util "../../util"
	helpers "../helpers"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type ValidatorPubKey struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type Validator struct {
	Address string          `json:"address"`
	PubKey  ValidatorPubKey `json:"pub_key"`
	Power   string          `json:"power"`
	Name    string          `json:"name"`
}

var conf *util.Config

func init() {
	conf = util.GetConfig()
}

//ExecStart=/usr/bin/tendermint node --proxy_app=kvstore --p2p.persistent_peers=167b80242c300bf0ccfb3ced3dec60dc2a81776e@165.227.41.206:26656,3c7a5920811550c04bf7a0b2f1e02ab52317b5e6@165.227.43.146:26656,303a1a4312c30525c99ba66522dd81cca56a361a@159.89.115.32:26656,b686c2a7f4b1b46dca96af3a0f31a6a7beae0be4@159.89.119.125:26656
func Build(details *db.DeploymentDetails, servers []db.Server,
	clients []*ssh.Client, buildState *state.BuildState) ([]string, error) {
	//Ensure that genesis file has same chain_id
	peers := []string{}
	validators := []Validator{}
	buildState.SetBuildSteps(1 + (details.Nodes * 4))
	buildState.SetBuildStage("Initializing the nodes")

	mux := sync.Mutex{}
	err := helpers.AllNodeExecCon(servers, buildState, func(serverNum int, localNodeNum int, absoluteNodeNum int) error {
		ip := servers[serverNum].Ips[localNodeNum]
		//init everything
		_, err := clients[serverNum].DockerExec(localNodeNum, "tendermint init")
		if err != nil {
			log.Println(err)
			return err
		}

		//Get the node id
		res, err := clients[serverNum].DockerExec(localNodeNum, "tendermint show_node_id")
		if err != nil {
			log.Println(err)
			return err
		}
		nodeId := res[:len(res)-1]

		mux.Lock()
		peers = append(peers, fmt.Sprintf("%s@%s:26656", nodeId, ip))
		mux.Unlock()

		//Get the validators
		res, err = clients[serverNum].DockerExec(localNodeNum, "cat /root/.tendermint/config/genesis.json")
		if err != nil {
			log.Println(err)
			return err
		}
		buildState.IncrementBuildProgress()
		var genesis map[string]interface{}
		err = json.Unmarshal([]byte(res), &genesis)

		validatorsRaw := genesis["validators"].([]interface{})
		for _, validatorRaw := range validatorsRaw {
			validator := Validator{}

			validatorData := validatorRaw.(map[string]interface{})

			err = util.GetJSONString(validatorData, "address", &validator.Address)
			if err != nil {
				log.Println(err)
				return err
			}

			validatorPubKeyData := validatorData["pub_key"].(map[string]interface{})

			err = util.GetJSONString(validatorPubKeyData, "type", &validator.PubKey.Type)
			if err != nil {
				log.Println(err)
				return err
			}

			err = util.GetJSONString(validatorPubKeyData, "value", &validator.PubKey.Value)

			err = util.GetJSONString(validatorData, "power", &validator.Power)
			if err != nil {
				log.Println(err)
				return err
			}

			err = util.GetJSONString(validatorData, "name", &validator.Name)
			if err != nil {
				log.Println(err)
				return err
			}
			mux.Lock()
			validators = append(validators, validator)
			mux.Unlock()
		}
		buildState.IncrementBuildProgress()
		return nil
	})
	if err != nil {
		log.Println(err)
		return nil, err
	}
	buildState.SetBuildStage("Propogating the genesis file")

	//distribute the created genensis file among the nodes
	err = helpers.CopyBytesToAllNodes(servers, clients, buildState,
		GetGenesisFile(validators), "/root/.tendermint/config/genesis.json")
	if err != nil {
		log.Println(err)
		return nil, err
	}

	buildState.SetBuildStage("Starting tendermint")
	err = helpers.AllNodeExecCon(servers, buildState, func(serverNum int, localNodeNum int, absoluteNodeNum int) error {
		defer buildState.IncrementBuildProgress()
		return clients[serverNum].DockerExecdLog(localNodeNum, fmt.Sprintf("tendermint node --proxy_app=kvstore --p2p.persistent_peers=%s",
			strings.Join(append(peers[:absoluteNodeNum], peers[absoluteNodeNum+1:]...), ",")))
	})
	return nil, err
}

func GetGenesisFile(validators []Validator) string {
	validatorsStr, _ := json.Marshal(validators)
	return fmt.Sprintf(`{
      "genesis_time": "%s",
      "chain_id": "whiteblock",
      "consensus_params": {
        "block_size": {
          "max_bytes": "22020096",
          "max_gas": "-1"
        },
        "evidence": {
          "max_age": "100000"
        },
        "validator": {
          "pub_key_types": [
            "ed25519"
          ]
        }
      },
      "validators": %s,
      "app_hash": "" 
    }`, time.Now().Format("2006-01-02T15:04:05.000000000Z"),
		validatorsStr)
}
