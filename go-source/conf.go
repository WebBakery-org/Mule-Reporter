package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/ini.v1"
)

const (
	Version    = "0.0.3"
	Codename   = "Provisionner"
	ConfigPath = "conf.ini"
)

const (
	SECTION_ENV = iota
	SECTION_PORTS
	SECTION_STORAGE
)

const (
	FIELD_TALKPORT = iota
	FIELD_ROOT_FS
	FIELD_MOUNTS_PATH
	FIELD_IMAGES_PATH
	FIELD_EXPORT_RANGE
	FIELD_NFS
	FIELD_MOUNT_OPTS
	FIELD_AUTHORIZED_IP
)

var CONFIG_SECTION = map[int]string{
	SECTION_ENV:     "ENV",
	SECTION_PORTS:   "PORTS",
	SECTION_STORAGE: "STORAGE",
}

var CONFIG_FIELDS = map[int]string{
	FIELD_TALKPORT:      "talkport",
	FIELD_ROOT_FS:       "root",
	FIELD_MOUNTS_PATH:   "mounts_path",
	FIELD_IMAGES_PATH:   "images_path",
	FIELD_EXPORT_RANGE:  "export_range",
	FIELD_NFS:           "nfs",
	FIELD_MOUNT_OPTS:    "mount_options",
	FIELD_AUTHORIZED_IP: "accept_cidr_range",
}

type DISK_TYPE uint8 // no need for more

const (
	SSD DISK_TYPE = 0 // doesn't turn
	HDD DISK_TYPE = 1 // turns (physically, like, the disk)
	ERR DISK_TYPE = 7 // error while reading disk informations (should not happen though)
)

// Suffix that, when contained in a mount point, indicates a partition that should not be used
// as storage.
var PARTITION_SUFFIX_FILTER []string = []string{
	"boot",
	"efi",
}

type ApiMessage struct {
	Message json.RawMessage `json:"message"`
	Error   string          `json:"error"`
}

type Node struct {
	RootDir  string     // Doesn't serialize, it's simply where the root directory of this machine is located from the root directory of this environment. By default with the conf.ini file, it's /host.
	Hostname string     `json:"hostname"`
	Version  string     `json:"version"`
	Codename string     `json:"codename"`
	Port     string     `json:"port"`
	Disks    []DiskInfo `json:"disks"`
}

type MuleStats struct {
	Timestamp int64 `json:"timestamp"`
	NodeInfo  Node  `json:"node"`
}

type DiskInfo struct {
	Name       string          `json:"name"`       // Disk's name
	Type       DISK_TYPE       `json:"type"`       // Disk's type (HDD = 1, SSD = 0) (based on disk rotation ability)
	Partitions []PartitionInfo `json:"partitions"` // Disk's partitions
}

type PartitionInfo struct {
	MountPoint string `json:"path"` // Disk mount point
	Size       uint64 `json:"size"` // Disk total size
	Available  uint64 `json:"free"` // Disk free space
}

func GetConfigPath() string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	// conf.ini must always be next to the executable
	return filepath.Join(exeDir, ConfigPath)
}

var cachedConfig *ini.File = nil
var configOnce sync.Once

func GetConfig() *ini.File {
	if cachedConfig != nil {
		return cachedConfig
	}
	configOnce.Do(func() {
		config, err := ini.Load(GetConfigPath())
		if err != nil {
			fmt.Printf("Mule was drunk trying to get that damn config: %v\n", err)
			os.Exit(1)
		}
		cachedConfig = config
	})
	return cachedConfig
}

func GetConfigValue(section int, field int) (string, error) {
	conf := GetConfig()
	if !conf.HasSection(CONFIG_SECTION[section]) {
		return "", fmt.Errorf("Configuration file doesn't contain any definition for this section, or it is not supported yet.\n")
	}
	if !conf.Section(CONFIG_SECTION[section]).HasKey(CONFIG_FIELDS[field]) {
		return "", fmt.Errorf("Configuration file doesn't contain any definition for this key in section %s, or it is not supported yet.\n", CONFIG_SECTION[section])
	}
	return conf.Section(CONFIG_SECTION[section]).Key(CONFIG_FIELDS[field]).Value(), nil
}

func IsIPAuthorized(IP string) bool {
	cidr, err := GetConfigValue(SECTION_ENV, FIELD_AUTHORIZED_IP)
	ReportError(err)
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		fmt.Printf("[!] Invalid CIDR mask specified in configuration file. Not letting anyone communicate.\n%v\n", err)
		return false
	}
	host, _, err := net.SplitHostPort(IP)
	if err != nil {
		host = IP // if IP came alone without a port (I guess why not)
	}
	// Time to know if the IP is authorized
	return ipNet.Contains(net.ParseIP(host))
}

func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80") // dials 8.8.8.8 to get real IP
	if err != nil {
		return "127.0.0.1" // Fallback just in case
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func NewNode() *Node {
	rootDir, err := GetConfigValue(SECTION_ENV, FIELD_ROOT_FS)
	ReportError(err)
	port, err := GetTalkPort()
	ReportError(err)
	node := &Node{
		RootDir:  rootDir,
		Hostname: GetOutboundIP(),
		Port:     port,
		Version:  Version,
		Codename: Codename,
	}
	disks, _ := GetDisks(node)
	node.Disks = disks
	return node
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

func RefreshPartitions(node *Node, disk *DiskInfo) ([]PartitionInfo, error) {
	mounts, err := os.Open(node.RootDir + "/proc/mounts")
	if err != nil {
		return nil, err
	}
	defer mounts.Close()
	scanner := bufio.NewScanner(mounts)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// not enough information to be exploitable
		if len(fields) < 2 {
			continue
		}
		// try to find the real device name
		realDevice, err := filepath.EvalSymlinks(node.RootDir + "/" + fields[0])
		if err != nil {
			// if that fails, tries again without prefixing with the environment's host
			realDevice, err = filepath.EvalSymlinks(fields[0])
			if err != nil {
				// if again, that fails, keep the mount point as the real device
				realDevice = fields[0]
			}
		}
		// fmt.Printf("DEBUG: Testing device %s for mount %s\n", realDevice, fields[1])
		devName := filepath.Base(realDevice)
		name := devName
		slavePath := node.RootDir + "/sys/block/" + devName + "/slaves"
		slaves, err := os.ReadDir(slavePath)
		if err == nil && len(slaves) > 0 {
			// On prend le premier esclave (ex: nvme0n1p3)
			devName = slaves[0].Name()
		}
		if strings.HasPrefix(devName, "sd") || strings.HasPrefix(devName, "vd") || strings.HasPrefix(devName, "hd") {
			// standard, virtual or hard disk
			name = devName[:3] // silly :3
		} else if strings.HasPrefix(name, "nvme") {
			name = strings.Split(devName, "p")[0]
		}
		if strings.Compare(disk.Name, name) != 0 {
			// This disk isn't the one we're searching for, maybe it is not mounted or not detected yet ?
			// Either way, skip to the next one
			continue
		}
		// if the mountpoint isn't relative to the container (if any), append it to get the absolute path
		if !strings.HasPrefix(fields[1], node.RootDir) {
			fields[1] = node.RootDir + fields[1]
		}
		fields[1] = strings.ReplaceAll(fields[1], "//", "/")

		// Disk name matches ! Let's complete the entry;
		var stat syscall.Statfs_t
		err = syscall.Statfs(fields[1], &stat)
		if err != nil {
			// error reading this partition's size, continue to next disk
			fmt.Println("'Failed to read path " + fields[1])
			continue
		}
		if len(node.RootDir) > 0 && strings.HasPrefix(fields[1], node.RootDir) {
			// if mule's config has been written in this field, remove it for clarity.
			fields[1] = strings.Replace(fields[1], node.RootDir, "", 1)
			if len(fields[1]) == 0 {
				// if the path is now empty, it's the root path.
				fields[1] = "/"
			}
		}
		// preventing duplicates before inserting
		present := false
		for index, partition := range disk.Partitions {
			if strings.HasPrefix(fields[1], partition.MountPoint) {
				present = true
				// no need to iterate through it all, we already know this partition is already listed.
				break
			} else if strings.HasPrefix(partition.MountPoint, fields[1]) {
				// we check for the opposite though, as if the partition we got first isn't the highest one, we shall get it still.
				// we then remove the lowest ranked one.
				disk.Partitions = append(disk.Partitions[:index], disk.Partitions[index+1:]...)
			}
		}
		if present {
			// we don't insert duplicates, as this would cause incorrect size formatting in the end.
			continue
		}
		disk.Partitions = append(disk.Partitions, PartitionInfo{
			MountPoint: fields[1],
			Size:       stat.Blocks * uint64(stat.Bsize),
			Available:  stat.Bavail * uint64(stat.Bsize),
		})
	}

	return disk.Partitions, err
}

func GetDisks(node *Node) ([]DiskInfo, error) {
	var devices []DiskInfo
	files, err := os.ReadDir(node.RootDir + "/sys/block")
	if err != nil {
		return nil, err
	}
	for _, directory := range files {
		name := directory.Name()

		// that type of disk is irrelevant
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}

		rotationalData, err := os.ReadFile(node.RootDir + "/sys/block/" + name + "/queue/rotational")

		// That disk is broken, don't count it and continue
		if err != nil {
			continue
		}

		diskType := ERR
		switch strings.TrimSpace(string(rotationalData)) {
		case "1":
			diskType = HDD
		case "0":
			diskType = SSD
		}
		diskInfo := DiskInfo{
			Name: name,
			Type: diskType,
		}
		RefreshPartitions(node, &diskInfo)
		if diskInfo.Partitions == nil {
			// if this disk has no partition,
			// it's not usable and shouldn't be shown. continue to the next disk.
			continue
		}
		devices = append(devices, diskInfo)
	}
	return devices, err
}

func GetAvailableSpace(path string) uint64 {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0
	}
	return stat.Bavail * uint64(stat.Bsize)
}

func GetTalkPort() (string, error) {
	port, err := GetConfigValue(SECTION_PORTS, FIELD_TALKPORT)
	if err != nil {
		return "", err
	}
	return port, nil
}
