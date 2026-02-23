package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"net/http"
	_ "net/http/pprof"
)

// Source - https://stackoverflow.com/a/37563128
// Posted by icza, modified by community. See post 'Timeline' for change history
// Retrieved 2026-02-21, License - CC BY-SA 4.0

func Filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

func AnswerStorage() []byte {
	this := NewNode()
	disks := this.Disks
	for diskIndex, disk := range this.Disks {
		disk.Partitions = Filter(disk.Partitions, func(pi PartitionInfo) bool {
			for _, filter := range PARTITION_SUFFIX_FILTER {
				if strings.HasSuffix(pi.MountPoint, filter) {
					return false
				}
			}
			return true
		})
		this.Disks[diskIndex].Partitions = disk.Partitions
	}
	disks = Filter(disks, func(di DiskInfo) bool { return len(di.Partitions) > 0 }) // don't keep empty disks.
	json, _ := json.Marshal(disks)
	return json
}

func InferFromMessage(msg []byte) any {
	// List of structs constructors
	candidates := []func() any{
		func() any { return &VolumeRequest{} },
	}

	for _, createTarget := range candidates {
		target := createTarget()

		// Disallow unknown fields, to be sure it's an exact match.
		decoder := json.NewDecoder(bytes.NewReader(msg))
		decoder.DisallowUnknownFields()

		if err := decoder.Decode(target); err == nil {
			return target // Got em
		}
	}

	return nil // No type matches
}

func main() {
	muleStart := time.Now()
	go func() {
		http.ListenAndServe(":555", nil)
	}()
	this := NewNode()
	port, err := GetTalkPort()
	ln, err := net.Listen("tcp", ":"+port)
	ReportError(err)
	AutoMount()
	fmt.Printf(
		"Mule-Reporter (%s-%s)\nis ready on:\nhost:\t\t%s : %s\nIn: %s\n",
		this.Version,
		this.Codename,
		this.Hostname,
		this.Port,
		time.Since(muleStart),
	)
	for {
		conn, _ := ln.Accept()
		go func(connection net.Conn) {
			defer connection.Close()
			if !IsIPAuthorized(connection.RemoteAddr().String()) {
				fmt.Printf("[🛡️] Unauthorized access attempt from %s - Rejected.\n", connection.RemoteAddr().String())
				errmsg := "The Mule deemed you not worthy of her grace."
				errmsg_json, _ := json.Marshal("The Mule deemed you not worthy of her grace.")
				message, _ := json.Marshal(ApiMessage{
					Message: errmsg_json,
					Error:   errmsg,
				})
				connection.Write(message)
				return
			}
			message, _ := bufio.NewReader(connection).ReadString('\n')
			answer := ApiMessage{}
			switch message {
			case "getme\n", "getme":
				answer.Message, err = json.Marshal(GetMuleBinary())
				if err != nil {
					answer.Error = err.Error()
					fmt.Printf("%v", err)
				}
			case "storage\n", "storage":
				// as opposed to disks, this one only return the "storage" ones (filters boot, efi, etc.)
				answer.Message = AnswerStorage()
			case "disk\n", "disk", "disks\n", "disks":
				// return all available disks and partitions
				disks, err := json.Marshal(this.Disks)
				if err != nil {
					answer.Error = err.Error()
				}
				answer.Message = disks
			case "what?\n", "what?", "what", "what\n":
				stats, err := json.Marshal(*Stats())
				if err != nil {
					answer.Error = err.Error()
				}
				answer.Message = stats
			default:
				// try to unmarshal JSON
				data := InferFromMessage([]byte(message))
				if data == nil {
					answer.Error = fmt.Errorf("Could not infer any type from your request.").Error()
				}
				switch object := data.(type) {
				case *VolumeRequest:
					err := CreateXFSVolume(this, *object)
					if err != nil {
						answer.Error = fmt.Errorf("An error happened while trying to create a logical volume: %w.", err).Error()
						fmt.Printf("An error occurred: %v\n", err)
					} else {
						answer.Message, _ = json.Marshal("Disk successfully created.")
						fmt.Println("No error occurred and disk was successfully created.")
					}
				case *VolumeRemovalRequest:
					err := RemoveDisk(this, *object)
					if err != nil {
						answer.Error = fmt.Errorf("An error happened while trying to remove a logical volume: %w.", err).Error()
						fmt.Printf("An error occurred: %v\n", err)
					} else {
						answer.Message, _ = json.Marshal("Disk successfully erased and disbound from NFS.")
						fmt.Println("No error occurred and disk was successfully removed.")
					}
				}
			}
			json, err := json.Marshal(answer)
			if err != nil {
				fmt.Printf("An error occurred while trying to answer from the Mule: %v", err)
				return
			}
			connection.Write(fmt.Append(json, "\n"))
		}(conn)
	}
}
