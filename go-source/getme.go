package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
)

// To get the current version of the Mule, from this Mule, to another one
type MuleBinary struct {
	Binary string `json:"base64_binary"`
	Script string `json:"installation_script"`
	Config string `json:"config"`
}

func GetMuleBinary() MuleBinary {
	return MuleBinary{
		Binary: base64.StdEncoding.EncodeToString(GetBinary()),
		Script: GetInstallationScript(),
		Config: GetConfigContent(),
	}
}

func GetInstallationScript() string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	scriptPath := filepath.Dir(exe) + "/service-subscribe.sh"
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	return string(script)
}

func GetConfigContent() string {
	script, err := os.ReadFile(GetConfigPath())
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	return string(script)
}

// Returns this executable's binary
func GetBinary() []byte {
	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("An error occured while getting the current directory: %v\n", err)
	}
	content, err := os.ReadFile(exe)
	if err != nil {
		fmt.Printf("An error occured while getting the binary content: %v\n", err)
	}
	return content
}
