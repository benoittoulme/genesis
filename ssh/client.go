// Package ssh contains abstractions which manage SSH connections for the user,
// allowing for faster and easier remote execution
package ssh

import (
	"../state"
	"../util"
	"context"
	"fmt"
	"github.com/Whiteblock/scp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/semaphore"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"time"
)

var conf = util.GetConfig()

const maxRunAttempts int = 20

const maxConnections int = 50

// Client maintains a persistent connect with a server,
// allowing commands to be run on that server. This object is thread safe.
type Client struct {
	clients  []*ssh.Client
	host     string
	serverID int
	mux      *sync.RWMutex
	sem      *semaphore.Weighted
}

// NewClient creates an instance of Client, with a connection to the
// host server given.
func NewClient(host string, serverID int) (*Client, error) {
	out := new(Client)
	for i := maxConnections; i > 0; i -= 5 {
		client, err := sshConnect(host)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		out.clients = append(out.clients, client)
	}
	out.host = host
	out.serverID = serverID
	out.mux = &sync.RWMutex{}
	out.sem = semaphore.NewWeighted(int64(maxConnections))
	return out, nil
}

func (sshClient *Client) getSession() (*Session, error) {
	sshClient.mux.RLock()
	ctx := context.TODO()
	sshClient.sem.Acquire(ctx, 1)
	for _, client := range sshClient.clients {
		session, err := client.NewSession()
		if err != nil {
			continue
		}
		sshClient.mux.RUnlock()
		return NewSession(session, sshClient.sem), nil
	}
	sshClient.mux.RUnlock()

	client, err := sshConnect(sshClient.host)
	for err != nil && (strings.Contains(err.Error(), "connection reset by peer") || strings.Contains(err.Error(), "EOF")) {
		log.Println(err)
		time.Sleep(50 * time.Millisecond)
		client, err = sshConnect(sshClient.host)
	}
	if client == nil {
		sshClient.sem.Release(1)
		return nil, fmt.Errorf("error(\"%s\"): client is nil", err.Error())
	}
	if err != nil {
		sshClient.sem.Release(1)
		log.Println(err)
		return nil, err
	}
	session, err := client.NewSession()
	if err != nil {
		sshClient.sem.Release(1)
		log.Println(err)
		return nil, err
	}
	sshClient.mux.Lock()
	sshClient.clients = append(sshClient.clients, client)
	sshClient.mux.Unlock()
	return NewSession(session, sshClient.sem), nil
}

// MultiRun provides an easy shorthand for multiple calls to sshExec
func (sshClient *Client) MultiRun(commands ...string) ([]string, error) {

	out := []string{}
	for _, command := range commands {

		res, err := sshClient.Run(command)
		if err != nil {
			return nil, err
		}
		out = append(out, string(res))
	}
	return out, nil
}

// FastMultiRun speeds up remote execution by chaining commands together
func (sshClient *Client) FastMultiRun(commands ...string) (string, error) {

	cmd := ""
	for i, command := range commands {

		if i != 0 {
			cmd += "&&"
		}
		cmd += command
	}
	return sshClient.Run(cmd)
}

// Run executes a given command on the connected remote machine.
func (sshClient *Client) Run(command string) (string, error) {
	session, err := sshClient.getSession()
	if conf.Verbose {
		fmt.Printf("Running command: %s\n", command)
	}
	bs := state.GetBuildStateByServerID(sshClient.serverID)
	defer session.Close()
	if bs.Stop() {
		return "", bs.GetError()
	}

	if err != nil {
		log.Println(err)
		return "", err
	}

	out, err := session.Get().CombinedOutput(command)
	if conf.Verbose {
		fmt.Println(string(out))
	}
	if err != nil {
		return string(out), util.FormatError(string(out), err)
	}
	return string(out), nil
}

// KeepTryRun attempts to run a command successfully multiple times. It will
// keep trying until it reaches the max amount of tries or it is successful once.
func (sshClient *Client) KeepTryRun(command string) (string, error) {
	var res string
	var err error
	bs := state.GetBuildStateByServerID(sshClient.serverID)
	if bs.Stop() {
		return "", bs.GetError()
	}
	for i := 0; i < maxRunAttempts; i++ {
		res, err = sshClient.Run(command)
		if err == nil {
			break
		}
	}
	return res, err
}

// DockerExec executes a command inside of a node
func (sshClient *Client) DockerExec(node int, command string) (string, error) {
	return sshClient.Run(fmt.Sprintf("docker exec %s%d %s", conf.NodePrefix, node, command))
}

// DockerCp copies a file on a remote machine from source to the dest in the node
func (sshClient *Client) DockerCp(node int, source string, dest string) error {
	_, err := sshClient.Run(fmt.Sprintf("docker cp %s %s%d:%s", source, conf.NodePrefix, node, dest))
	return err
}

// KeepTryDockerExec is like KeepTryRun for nodes
func (sshClient *Client) KeepTryDockerExec(node int, command string) (string, error) {
	return sshClient.KeepTryRun(fmt.Sprintf("docker exec %s%d %s", conf.NodePrefix, node, command))
}

// KeepTryDockerExecAll is like KeepTryRun for nodes, but can handle more than one command.
// Executes the given commands in order.
func (sshClient *Client) KeepTryDockerExecAll(node int, commands ...string) ([]string, error) {
	out := []string{}
	for _, command := range commands {
		res, err := sshClient.KeepTryRun(fmt.Sprintf("docker exec %s%d %s", conf.NodePrefix, node, command))
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// DockerExecd runs the given command, and then returns immediately.
// This function will not return the output of the command.
// This is useful if you are starting a persistent process inside a container
func (sshClient *Client) DockerExecd(node int, command string) (string, error) {
	return sshClient.Run(fmt.Sprintf("docker exec -d %s%d %s", conf.NodePrefix, node, command))
}

// DockerExecdit runs the given command, and then returns immediately.
// This function will not return the output of the command.
// This is useful if you are starting a persistent process inside a container.
// Also flags the session as interactive and sets up a virtual tty.
func (sshClient *Client) DockerExecdit(node int, command string) (string, error) {
	return sshClient.Run(fmt.Sprintf("docker exec -itd %s%d %s", conf.NodePrefix, node, command))
}

func (sshClient *Client) logSanitizeAndStore(node int, command string) {
	if strings.Count(command, "'") != strings.Count(command, "\\'") {
		panic("DockerExecdLog commands cannot contain unescaped ' characters")
	}
	bs := state.GetBuildStateByServerID(sshClient.serverID)
	bs.Set(fmt.Sprintf("%d", node), util.Command{Cmdline: command, ServerID: sshClient.serverID, Node: node})
}

// DockerExecdLog will cause the stdout and stderr of the command to be stored in the logs.
// Should only be used for the blockchain process.
func (sshClient *Client) DockerExecdLog(node int, command string) error {
	sshClient.logSanitizeAndStore(node, command)

	_, err := sshClient.Run(fmt.Sprintf("docker exec -d %s%d bash -c '%s 2>&1 > %s'", conf.NodePrefix,
		node, command, conf.DockerOutputFile))
	return err
}

// DockerExecdLogAppend will cause the stdout and stderr of the command to be stored in the logs.
// Should only be used for the blockchain process. Will append to existing logs.
func (sshClient *Client) DockerExecdLogAppend(node int, command string) error {
	sshClient.logSanitizeAndStore(node, command)
	_, err := sshClient.Run(fmt.Sprintf("docker exec -d %s%d bash -c '%s 2>&1 >> %s'", conf.NodePrefix,
		node, command, conf.DockerOutputFile))
	return err
}

// DockerRead will read a file on a node, if lines > -1 then
// it will return the last `lines` lines of the file
func (sshClient *Client) DockerRead(node int, file string, lines int) (string, error) {
	if lines > -1 {
		return sshClient.Run(fmt.Sprintf("docker exec %s%d tail -n %d %s", conf.NodePrefix, node, lines, file))
	}
	return sshClient.Run(fmt.Sprintf("docker exec %s%d cat %s", conf.NodePrefix, node, file))
}

// DockerMultiExec will run all of the given commands strung together with && on
// the given node.
func (sshClient *Client) DockerMultiExec(node int, commands []string) (string, error) {
	mergedCommand := ""

	for _, command := range commands {
		if len(mergedCommand) != 0 {
			mergedCommand += "&&"
		}
		mergedCommand += fmt.Sprintf("docker exec -d %s%d %s", conf.NodePrefix, node, command)
	}

	return sshClient.Run(mergedCommand)
}

// KTDockerMultiExec is like DockerMultiExec, except it keeps attempting the command after
// failure
func (sshClient *Client) KTDockerMultiExec(node int, commands []string) (string, error) {
	mergedCommand := ""

	for _, command := range commands {
		if len(mergedCommand) != 0 {
			mergedCommand += "&&"
		}
		mergedCommand += fmt.Sprintf("docker exec -d %s%d %s", conf.NodePrefix, node, command)
	}

	return sshClient.KeepTryRun(mergedCommand)
}

// Scp is a wrapper for the scp command. Can be used to copy
// a file over to a remote machine.
func (sshClient *Client) Scp(src string, dest string) error {
	if conf.Verbose {
		fmt.Printf("Remote copying %s to %s...", src, dest)
	}
	if !strings.HasPrefix(src, "./") && src[0] != '/' {
		bs := state.GetBuildStateByServerID(sshClient.serverID)
		src = "/tmp/" + bs.BuildID + "/" + src
	}

	session, err := sshClient.getSession()
	if err != nil {
		return err
	}
	defer session.Close()

	err = scp.CopyPath(src, dest, session.Get())
	if err != nil {
		return err
	}

	if conf.Verbose {
		fmt.Printf("done\n")
	}

	return nil
}

// InternalScp is a wrapper for the scp command. Can be used to copy
// a file over to a remote machine. This is for internal use, and may cause
// unpredictible behavior. Use Scp instead
func (sshClient *Client) InternalScp(src string, dest string) error {
	if conf.Verbose {
		fmt.Printf("Remote copying %s to %s...", src, dest)
	}

	session, err := sshClient.getSession()
	if err != nil {
		return err
	}
	defer session.Close()

	err = scp.CopyPath(src, dest, session.Get())
	if err != nil {
		return err
	}

	if conf.Verbose {
		fmt.Printf("done\n")
	}

	return nil
}

/*
   Scpr copies over a directory to a specified path on a remote host

func (sshClient Client) Scpr(dir string) error {

	path := GetPath(dir)
	_, err := sshClient.Run("mkdir -p " + path)
	if err != nil {
		log.Println(err)
		return err
	}

	file := fmt.Sprintf("%s.tar.gz", dir)
	_, err = BashExec(fmt.Sprintf("tar cfz %s %s", file, dir))
	if err != nil {
		log.Println(err)
		return err
	}
	err = sshClient.Scp(file, file)
	if err != nil {
		log.Println(err)
		return err
	}
	_, err = sshClient.Run(fmt.Sprintf("tar xfz %s && rm %s", file, file))
	return err
}*/

// Close cleans up the resources used by sshClient object
func (sshClient *Client) Close() {
	sshClient.mux.Lock()
	defer sshClient.mux.Unlock()
	for _, client := range sshClient.clients {
		if client == nil {
			continue
		}
		client.Close()
	}
}

func sshConnect(host string) (*ssh.Client, error) {

	key, err := ioutil.ReadFile(conf.SSHKey)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Println(err)
		return nil, err
	}
	sshConfig := &ssh.ClientConfig{
		User: conf.SSHUser,
		Auth: []ssh.AuthMethod{
			// Use the PublicKeys method for remote authentication.
			ssh.PublicKeys(signer),
		},
	}
	sshConfig.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	client, err := ssh.Dial("tcp", fmt.Sprintf("%s:22", host), sshConfig)
	i := 0
	for err != nil && i < 10 {
		client, err = ssh.Dial("tcp", fmt.Sprintf("%s:22", host), sshConfig)
		i++
	}
	if err != nil {
		log.Println(err)
		return nil, err
	}

	return client, nil
}