package db

import(
	_ "github.com/mattn/go-sqlite3"
	"database/sql"
	"fmt"
	"errors"
	util "../util"
)

//TODO: Fix broken naming convention
type Node struct {	
	Id			int 	
	TestNetId	int		
	Server		int		
	LocalId		int		
	Ip			string	
}


func GetAllNodesByServer(serverId int) []Node {
	db := getDB()
	defer db.Close()

	rows, err :=  db.Query(fmt.Sprintf("SELECT id,test_net,server,local_id,ip FROM %s WHERE server = %d",NodesTable ))
	util.CheckFatal(err)
	defer rows.Close()
	
	nodes := []Node{}
	for rows.Next() {
		var node Node
		util.CheckFatal(rows.Scan(&node.Id,&node.TestNetId,&node.Server,&node.LocalId,&node.Ip))
		nodes = append(nodes,node)
	}
	return nodes
}

func GetAllNodesByTestNet(testId int) ([]Node,error) {
	db := getDB()
	defer db.Close()
	nodes := []Node{}

	rows, err :=  db.Query(fmt.Sprintf("SELECT id,test_net,server,local_id,ip FROM %s WHERE test_net = %d",NodesTable,testId ))
	if err != nil {
		return nodes,err
	}
	defer rows.Close()

	
	for rows.Next() {
		var node Node
		err := rows.Scan(&node.Id,&node.TestNetId,&node.Server,&node.LocalId,&node.Ip)
		if err != nil {
			return nodes, err
		}
		nodes = append(nodes,node)
	}
	return nodes, nil
}

func GetAllNodes() []Node {
	
	db := getDB()
	defer db.Close()

	rows, err :=  db.Query(fmt.Sprintf("SELECT id,test_net,server,local_id,ip FROM %s",NodesTable ))
	util.CheckFatal(err)
	defer rows.Close()
	nodes := []Node{}

	for rows.Next() {
		var node Node
		util.CheckFatal(rows.Scan(&node.Id,&node.TestNetId,&node.Server,&node.LocalId,&node.Ip))
		nodes = append(nodes,node)
	}
	return nodes
}

func GetNode(id int) (Node,error) {
	db := getDB()
	defer db.Close()

	row :=  db.QueryRow(fmt.Sprintf("SELECT id,test_net,server,local_id,ip FROM %s WHERE id = %d",NodesTable,id))

	var node Node

	if row.Scan(&node.Id,&node.TestNetId,&node.Server,&node.LocalId,&node.Ip) == sql.ErrNoRows {
		return node, errors.New("Not Found")
	}

	return node, nil
}

func InsertNode(node Node) (int,error) {
	db := getDB()
	defer db.Close()

	tx,err := db.Begin()
	if err != nil {
		return -1, err
	}

	stmt,err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (test_net,server,local_id,ip) VALUES (?,?,?,?)",NodesTable))
	
	if err != nil {
		return -1, err
	}

	defer stmt.Close()

	res,err := stmt.Exec(node.TestNetId,node.Server,node.LocalId,node.Ip)
	if err != nil {
		return -1, nil
	}
	
	tx.Commit()
	id, err := res.LastInsertId()
	return int(id), err
}


func DeleteNode(id int) error {
	db := getDB()
	defer db.Close()

	_,err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE id = %d",NodesTable,id))
	return err
}

func DeleteNodesByTestNet(id int) error {
	db := getDB()
	defer db.Close()

	_,err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE test_net = %d",NodesTable,id))
	return err
}	

func DeleteNodesByServer(id int) error {
	db := getDB()
	defer db.Close()

	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE server = %d",NodesTable,id))
	return err
}


/*******COMMON QUERY FUNCTIONS********/

func GetAvailibleNodes(serverId int, nodesRequested int) []int{

	nodes := GetAllNodesByServer(serverId)
	server,_,_ := GetServer(serverId)

	out := util.IntArrFill(server.Max,func(index int) int{
		return index
	})

	for _,node := range nodes {
		out = util.IntArrRemove(out,node.Id)
	}
	return out;
}