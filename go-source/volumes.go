package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Everything in this file will NOT work in a Swarm cluster.
// If you need to use these functions, use the standalone service deployment instead.

type VolumeRequest struct {
	Server     int    `json:"server"`     // used to compute the xfs project ID
	User       int    `json:"user"`       // used to compute the xfs project ID
	ImagePath  string `json:"image_path"` // where to store the .img file
	MountPath  string `json:"mount_path"` // where to mount the created disk
	SizeHuman  string `json:"size"`       // ex: "1G"
	CustomName string `json:"name"`
}

type VolumeRemovalRequest struct {
	ImagePath string `json:"image_path"` // where is stored the .img file
	MountPath string `json:"mount_path"` // where is mounted the created disk
	Name      string `json:"name"`       // The Disk's name
}

func RefreshNFS() error {
	if err := exec.Command("exportfs", "-ar").Run(); err != nil {
		return err
	}
	return nil
}

func RemoveDisk(
	// will be used in future stable versions
	node *Node,
	request VolumeRemovalRequest,
) error {
	nfs, err := GetConfigValue(SECTION_STORAGE, FIELD_NFS)
	ReportError(err)
	if nfs == "true" {
		// remove nfs route too
		if err := os.Remove("/etc/exports.d/" + request.Name + ".exports"); err != nil {
			return err
		}
	}

	if err := RefreshNFS(); err != nil {
		return err
	}

	if err := exec.Command("umount", request.MountPath).Run(); err != nil {
		return err
	}

	if err := os.RemoveAll(request.MountPath); err != nil {
		return err
	}

	return os.Remove(request.ImagePath)
}

// Returns true if a path is already mounted
func IsMounted(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	return strings.Contains(string(data), path)
}

// Mounts every subfolder from the one given in configuration file, if any
func AutoMount() {
	if !GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).HasKey(CONFIG_FIELDS[FIELD_MOUNTS_PATH]) {
		fmt.Println("The Mule couldn't find any path to auto-mount after reboot.\nLook into your configuration file or ignore this message if it's intentional.")
		return
	}
	if !GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).HasKey(CONFIG_FIELDS[FIELD_IMAGES_PATH]) {
		fmt.Println("The Mule couldn't find any volume image after reboot.\nLook into your configuration file or ignore this message if it's intentional.")
		return
	}
	timer := time.Now()
	imgRoot, err := GetConfigValue(SECTION_STORAGE, FIELD_IMAGES_PATH)
	ReportError(err)
	mntRoot, err := GetConfigValue(SECTION_STORAGE, FIELD_MOUNTS_PATH)
	ReportError(err)

	var waitGroup sync.WaitGroup

	err = filepath.WalkDir(imgRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Look for .img files
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".img") {
			waitGroup.Add(1)

			go func(path string, d string) {
				defer waitGroup.Done()
				// 1. Compute relative path to imgRoot
				// If path is "/srv/volumes/1/1/disk1.img"
				// relPath becomes "1/1/disk1.img"
				relPath, _ := filepath.Rel(imgRoot, path)

				// 2. Get mount point
				// removes .img extension and craft final path
				// mntDir becomes "/srv/clients/1/1/disk1"
				subPath := strings.TrimSuffix(relPath, ".img")
				mntDir := filepath.Join(mntRoot, subPath)

				// creates destination folder (X/Y/Z)
				os.MkdirAll(mntDir, 0755)

				// Mount if needed
				if !IsMounted(mntDir) {
					fmt.Printf("[Mule] Restauration : %s -> %s\n", relPath, mntDir)
					mountOpts, err := GetConfigValue(SECTION_STORAGE, FIELD_MOUNT_OPTS)
					if err != nil {
						fmt.Printf("[!] Mount error: %v\n", err)
					} else {
						err = exec.Command("mount", "-o", mountOpts, path, mntDir).Run()
						if err != nil {
							fmt.Printf("[!] Mount error %s: %v\n", relPath, err)
						}
					}
				}
			}(path, d.Name())
		}
		return nil
	})
	waitGroup.Wait()

	if err != nil {
		fmt.Printf("Error while scanning volumes : %v\n", err)
	}

	RefreshNFS()
	fmt.Printf("Remounted every volume, and refreshed NFS in %s.\n", time.Since(timer))
}

// Adds a new route in /etc/exports.d/XXXXX where XXXXX is the disk's name, from the given path.
func AddNFSRoute(
	mountPath string,
	diskName string,
) error {
	exportsDir := "/etc/exports.d/"
	if !GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).HasKey(CONFIG_FIELDS[FIELD_EXPORT_RANGE]) {
		return fmt.Errorf("Given conf.ini file doesn't provide a visibility key, but NFS is required.\nPlease configure a visibility key with a CIDR value (e.g. 192.168.1.0/24)")
	}
	networkVisibility := GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).Key(CONFIG_FIELDS[FIELD_EXPORT_RANGE]).Value()
	if err := os.MkdirAll(exportsDir, 0755); err != nil {
		return err
	}

	// checks file before exporting it in NFS
	if _, err := os.Stat(mountPath); os.IsNotExist(err) {
		return err
	}

	if err := os.WriteFile(exportsDir+diskName+".exports",
		[]byte(mountPath+" "+networkVisibility+"(rw,sync,no_root_squash,no_subtree_check)\n"),
		0755,
	); err != nil {
		return err
	}
	return RefreshNFS()
}

func parseHumanSize(human string) uint64 {
	human = strings.TrimSpace(strings.ToLower(human))
	re := regexp.MustCompile(`^([0-9.]+)\s*([a-z]*)$`)
	matches := re.FindStringSubmatch(human)

	if len(matches) != 3 {
		return 0
	}

	value, _ := strconv.ParseFloat(matches[1], 64)
	unit := matches[2]

	var multiplier float64 = 1

	switch unit {
	case "kb", "k":
		multiplier = 1000
	case "kib", "ki":
		multiplier = 1024
	case "mb", "m":
		multiplier = 1e6
	case "mib", "mi":
		multiplier = 1024 * 1024
	case "gb", "g":
		multiplier = 1e9
	case "gib", "gi":
		multiplier = 1024 * 1024 * 1024
	case "tb", "t":
		multiplier = 1e12
	case "tib", "ti":
		multiplier = 1024 * 1024 * 1024 * 1024
	}

	return uint64(value * multiplier)
}

func applyQuota(mntDir string, projectID int, hardHuman string) error {
	// Cleaning up the Unit for XFS (ex: 1GiB -> 1G)
	re := regexp.MustCompile(`(?i)([0-9.]+)\s*([a-z])`)
	matches := re.FindStringSubmatch(hardHuman)

	cleanUnit := "g" // default
	var num float64 = 1

	if len(matches) == 3 {
		num, _ = strconv.ParseFloat(matches[1], 64)
		cleanUnit = strings.ToUpper(matches[2][:1])
	}

	softNum := num * 0.8
	if softNum < 1 {
		softNum = 1
	}

	xfsSoft := fmt.Sprintf("%.0f%s", softNum, cleanUnit)
	xfsHard := fmt.Sprintf("%.0f%s", num, cleanUnit)

	// XFS Project init
	// xfs_quota -x -c "project -s -p path ID" path
	projCmd := exec.Command("xfs_quota", "-x", "-c",
		fmt.Sprintf("project -s -p %s %d", mntDir, projectID), mntDir)
	if err := projCmd.Run(); err != nil {
		return fmt.Errorf("failed to init xfs project: %w", err)
	}

	// Applying limits
	// xfs_quota -x -c "limit -p bsoft=XX bhard=YY ID" path
	limitCmd := exec.Command("xfs_quota", "-x", "-c",
		fmt.Sprintf("limit -p bsoft=%s bhard=%s %d", xfsSoft, xfsHard, projectID), mntDir)

	if err := limitCmd.Run(); err != nil {
		return fmt.Errorf("failed to set xfs limits: %w", err)
	}

	return nil
}

func CreateXFSVolume(node *Node, req VolumeRequest) error {
	// Computing path
	imgDir := req.ImagePath
	os.MkdirAll(imgDir, 0755)

	// XFS project ID
	projectID := 1000 + 0 + (req.Server * 100) + (req.User * 10000)

	imgFile := fmt.Sprintf("%s/%s.img", imgDir, req.CustomName)
	mntDir := fmt.Sprintf("%s/%s", req.MountPath, req.CustomName)
	os.MkdirAll(mntDir, 0755)

	// File already exists, aborting
	if _, err := os.Stat(imgFile); !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Truncate: creating .img file
	f, err := os.Create(imgFile)
	if err != nil {
		return fmt.Errorf("Failed to create img file: %w", err)
	}
	sizeInBytes := parseHumanSize(req.SizeHuman)
	if err := f.Truncate(int64(sizeInBytes)); err != nil {
		return fmt.Errorf("Failed to truncate file: %w", err)
	}
	f.Close()
	// XFS formatting
	if err := exec.Command("mkfs.xfs", "-n", "ftype=1", "-f", imgFile).Run(); err != nil {
		return fmt.Errorf("An error happened with mkfs tool: %w", err)
	}

	// Loop mount with Quota
	// IMPORTANT: Mule needs CAP_SYS_ADMIN privileges
	if !GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).HasKey(CONFIG_FIELDS[FIELD_MOUNT_OPTS]) {
		return fmt.Errorf("[!] An error occurred: mount_options key is not found in configuration file. It's needed when auto-mounting volumes.\n")
	}
	mountOpts := GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).Key(CONFIG_FIELDS[FIELD_MOUNT_OPTS]).Value()
	if err := exec.Command("mount", "-o", mountOpts, imgFile, mntDir).Run(); err != nil {
		return fmt.Errorf("An error happened when mounting the disk: %w", err)
	}

	dockerVolName := fmt.Sprintf("webbakery_%d_%d_%s", req.User, req.Server, req.CustomName)
	hostIP := GetOutboundIP()

	// On prépare les options NFS exactement comme dans ton script Bash
	// Attention : device=":${MNT_DIR}" utilise le chemin interne du conteneur,
	// assure-toi que mntDir est bien mappé à l'identique sur l'hôte et la Mule.
	if err := exec.Command("docker", "volume", "create", "--driver", "local",
		"--opt", "type=nfs",
		"--opt", fmt.Sprintf("device=:%s", mntDir),
		"--opt", fmt.Sprintf("o=addr=%s,rw,nolock,hard,nfsvers=4,proto=tcp", hostIP),
		dockerVolName).Run(); err != nil {
		return fmt.Errorf("docker volume create failed: %w", err)
	}

	// If this Mule is configured to add NFS routes, do it
	if GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).HasKey(CONFIG_FIELDS[FIELD_NFS]) &&
		GetConfig().Section(CONFIG_SECTION[SECTION_STORAGE]).Key(CONFIG_FIELDS[FIELD_NFS]).Value() == "true" {
		AddNFSRoute(mntDir, req.CustomName)
	}
	fmt.Printf("[wb] Volume %s created and mounted on %s\n", dockerVolName, hostIP)

	// Applying XFS Quotas
	return applyQuota(mntDir, projectID, req.SizeHuman)
}
