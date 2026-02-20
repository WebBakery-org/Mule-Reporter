package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	"gopkg.in/ini.v1"
)

const (
	Version  = "0.0.1"
	Codename = "Drunk"
)

type Node struct {
	Hostname string `json:"hostname"`
	Version  string `json:"version"`
	Codename string `json:"codename"`
	Port     string `json:"port"`
}

type MuleStats struct {
	Timestamp int64 `json:"timestamp"`
	NodeInfo  Node  `json:"node"`
}

func NewNode() *Node {
	hostname, err := os.Hostname()
	ReportError(err)
	return &Node{
		Hostname: hostname,
		Port:     GetTalkPort(),
		Version:  Version,
		Codename: Codename,
	}
}

func Stats() *MuleStats {
	return &MuleStats{
		Timestamp: time.Now().Unix(),
		NodeInfo:  *NewNode(),
	}
}

func ReportError(err error) {
	if err != nil {
		fmt.Printf("Mule-Reporter ran into a wall: %v\n", err)
		os.Exit(1)
	}
}

func GetAvailableSpace(path string) uint64 {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}

func GetTalkPort() string {
	config, err := ini.Load("conf.ini")
	if err != nil {
		fmt.Printf("Mule was drunk trying to get that damn config: %v\n", err)
		os.Exit(1)
	}
	return config.Section("PORT_CONFIG").Key("talkport").Value()

}

func main() {
	this := NewNode()
	port := GetTalkPort()
	ln, err := net.Listen("tcp", ":"+port)
	ReportError(err)
	fmt.Printf(
		"Mule-Reporter (%s-%s)\nis ready on:\nhost:\t\t\t%s\non port:\t\t%s\n",
		this.Version,
		this.Codename,
		this.Hostname,
		this.Port,
	)
	for {
		conn, _ := ln.Accept()

		go func(connection net.Conn) {
			defer connection.Close()
			message, _ := bufio.NewReader(connection).ReadString('\n')
			switch message {
			case "size\n", "size":
				space := GetAvailableSpace("/host")
				response := fmt.Sprintf("{\"size\": %d}", space)
				connection.Write([]byte(response))
			case "what?\n", "what?", "what", "what\n":
				stats, _ := json.Marshal(*Stats())
				connection.Write([]byte(stats))
			default:
				connection.Write([]byte("Cant understand a damn thing you say.\n"))
			}
		}(conn)
	}
}
