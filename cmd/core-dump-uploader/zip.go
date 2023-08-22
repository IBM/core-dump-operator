package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type ZippedCoreDump interface {
	IsValidFile(filePath string) bool
	Begin(filePath string) error
	End()
	ParseRuntimeJsonBuf(buf []byte) (namespace string, err error)
	ExtractRuntimeJson(filePath string) ([]byte, error)
	GetNamespace() (namespace string)
	GetFile() *os.File
}

type ZippedCoreDumpImpl struct {
	defaultNamespace string
	f                *os.File
	flocked          bool
}

func NewZippedCoreDump(defaultNamespace string) ZippedCoreDump {
	return &ZippedCoreDumpImpl{defaultNamespace: defaultNamespace}
}

func (z *ZippedCoreDumpImpl) IsValidFile(filePath string) bool {
	if !strings.HasSuffix(filePath, ".zip") {
		return false
	}
	if _, err := os.Stat(filePath); err != nil {
		return false
	}
	return true
}

func (z *ZippedCoreDumpImpl) Begin(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed: Open, filePath=%v, err=%v", filePath, err)
	}
	z.f = f

	// wait until core-dump-composer fills the file
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("failed: Flock, filePath=%v, err=%v", filePath, err)
	}
	z.flocked = true
	return nil
}

func (z *ZippedCoreDumpImpl) End() {
	if z.flocked && z.f != nil {
		if err := unix.Flock(int(z.f.Fd()), unix.LOCK_UN); err != nil {
			log.Printf("WARN: Flock (LOCK_UN), filePath=%v, err=%v", z.f.Name(), err)
		}
		z.flocked = false
	}
	if z.f != nil {
		if err := os.Remove(z.f.Name()); err != nil {
			if !os.IsNotExist(err) {
				log.Printf("Failed: coult not remove file %v, err=%v", z.f.Name(), err)
			}
		}
		if err := z.f.Close(); err != nil {
			log.Printf("WARN: Close, filePath=%v, err=%v", z.f.Name(), err)
		}
		z.f = nil
	}
}

func (z *ZippedCoreDumpImpl) ParseRuntimeJsonBuf(buf []byte) (namespace string, err error) {
	var v map[string]interface{}
	if err = json.Unmarshal(buf, &v); err != nil {
		return "", fmt.Errorf("failed: json.Unmarshal, err=%v", err)
	}
	var ok = false
	status, ok1 := v["status"]
	if ok1 {
		if v2, ok2 := status.(map[string]interface{}); ok2 {
			if metadata, ok3 := v2["metadata"]; ok3 {
				if v4, ok4 := metadata.(map[string]interface{}); ok4 {
					if v5, ok5 := v4["namespace"]; ok5 {
						namespace, ok = v5.(string)
					}
				}
			}
		}
	}
	if !ok {
		return "", fmt.Errorf("failed: ParseRuntimeJsonBuf, no entry for status.metadata.namespace")
	}
	return
}

func (z *ZippedCoreDumpImpl) ExtractRuntimeJson(filePath string) ([]byte, error) {
	f, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed: ExtractRuntimeJson, OpenReader, filePath=%v, err=%v", filePath, err)
	}
	var buf []byte = nil
	for _, file := range f.File {
		if !strings.HasSuffix(filepath.Base(file.Name), "-runtime-info.json") {
			continue
		}
		var f2 io.ReadCloser = nil
		f2, err = file.Open()
		if err != nil {
			return nil, fmt.Errorf("failed: ExtractRuntimeJson, Open, filePath=%v, file.Name=%v, err=%v", filePath, file.Name, err)
		}
		buf, err = io.ReadAll(f2)
		f2.Close()
		if err != nil {
			return nil, fmt.Errorf("failed: ExtractRuntimeJson, ReadAll, filePath=%v, file.Name=%v, err=%v", filePath, file.Name, err)
		}
		break
	}
	if buf == nil {
		return nil, fmt.Errorf("failed: ExtractRuntimeJson, file does not containe -runtime-info.json, filePath=%v", filePath)
	}
	return buf, nil
}

func (z *ZippedCoreDumpImpl) GetNamespace() (namespace string) {
	if z.f == nil {
		log.Printf("WARN: GetNamespace, closed, use default namespace (%v)", z.defaultNamespace)
		return z.defaultNamespace
	}
	buf, err := z.ExtractRuntimeJson(z.f.Name())
	if err != nil {
		log.Printf("WARN: GetNamespace, ExtractRuntimeJson, use default namespace (%v), z.f.Name()=%v, err=%v", z.defaultNamespace, z.f.Name(), err)
		return z.defaultNamespace
	}
	namespace, err = z.ParseRuntimeJsonBuf(buf)
	if err != nil {
		log.Printf("WARN: GetNamespace, ParseRuntimeJsonBuf, use default namespace (%v), err=%v", z.defaultNamespace, err)
		return z.defaultNamespace
	}
	return namespace
}

func (z *ZippedCoreDumpImpl) GetFile() *os.File {
	return z.f
}
