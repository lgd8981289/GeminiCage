//go:build ignore

// Generate the NoCloud seed for this offline Debian boot check.
package main

import (
	"log"
	"os"
	"strings"

	"github.com/lima-vm/lima/v2/pkg/iso9660util"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("请指定测试 ISO 的输出路径")
	}
	if _, err := os.Lstat(os.Args[1]); !os.IsNotExist(err) {
		log.Fatal("测试 ISO 必须使用新的文件名")
	}
	const config = `#cloud-config
bootcmd:
  - |
    . /etc/os-release
    printf 'GEMINI_CAGE_BOOT_OK %s %s\n' "$VERSION_ID" "$(uname -m)" > /dev/ttyS0
`
	err := iso9660util.Write(os.Args[1], "cidata", []iso9660util.Entry{
		{Path: "meta-data", Reader: strings.NewReader("instance-id: gemini-cage-offline-test\nlocal-hostname: gemini-cage-test\n")},
		{Path: "user-data", Reader: strings.NewReader(config)},
	})
	if err != nil {
		log.Fatal(err)
	}
}
