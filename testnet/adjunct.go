/*
	Copyright 2019 whiteblock Inc.
	This file is a part of the genesis.

	Genesis is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	Genesis is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package testnet

import (
	"github.com/whiteblock/genesis/db"
	"github.com/whiteblock/genesis/ssh"
	"github.com/whiteblock/genesis/state"
)

// Adjunct represents a part of the network which contains
// a class of sidecars.
type Adjunct struct {
	// Testnet is a pointer to the master testnet
	Main       *TestNet
	Index      int
	Nodes      []ssh.Node
	BuildState *state.BuildState //ptr to the main one
	LDD        *db.DeploymentDetails
}

// GetSCNodes gets all of the sidecar nodes with the same index
func (adj *Adjunct) GetSCNodes() []ssh.Node {
	return adj.Main.GetSSHNodes(false, true, adj.Index)
}

// GetNewSCNodes gets all of the new sidecar nodes with the same index
func (adj *Adjunct) GetNewSCNodes() []ssh.Node {
	return adj.Main.GetSSHNodes(true, true, adj.Index)
}
