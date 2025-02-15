package main

import (
	"os"
	"path/filepath"
	"strings"
)

// loadVMConfigs는 설정 파일들을 읽어 VMConfig 목록으로 반환합니다.
func loadVMConfigs(configDir string) []VMConfig {
	var configs []VMConfig
	confFiles, _ := filepath.Glob(filepath.Join(configDir, "*.conf"))
	for _, conf := range confFiles {
		data, err := os.ReadFile(conf)
		if err != nil {
			continue
		}
		config := VMConfig{}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			switch key {
			case "name":
				config.Name = value
			case "cpuModel":
				config.CPUModel = value
			}
		}
		configs = append(configs, config)
	}
	return configs
}