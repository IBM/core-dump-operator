package main

import (
	"archive/zip"
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func randString(size int) []byte {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("Failed: rand.Read at randString: err=%v", err)
	}

	ret := make([]byte, size)
	for i, v := range b {
		ret[i] = letters[int(v)%len(letters)]
	}
	return ret
}

func CreateRandomFile(t *testing.T, filePath string, size int64) error {
	bufSize := int(size)
	if bufSize > 1024 {
		bufSize = 1024
	}
	buf := randString(bufSize)

	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Errorf("Failed: CreateRandomFile, OpenFile, filePath=%v, err=%v", filePath, err)
		return err
	}
	defer f.Close()

	count := int64(0)
	for count < size {
		bufLen := size - count
		if bufLen > int64(len(buf)) {
			bufLen = int64(len(buf))
		}
		c, err := f.WriteAt(buf[:bufLen], count)
		if err != nil {
			t.Errorf("Failed: CreateRandomFile, WriteAt, filePath=%v, count=%v, size=%v, err=%v", filePath, count, size, err)
			return err
		}
		count += int64(c)
	}
	return nil
}

type RuntimeJson struct {
	Status map[string]map[string]interface{} `json:"status"`
	Dummy  map[string]string                 `json:"dummy"`
}

func GetRuntimeJsonBuf(t *testing.T, namespace string, malform int) ([]byte, error) {
	r := RuntimeJson{Status: make(map[string]map[string]interface{}), Dummy: make(map[string]string)}
	if malform != 0 {
		r.Status["metadata"] = make(map[string]interface{})
		if malform != 1 {
			if malform != 2 {
				r.Status["metadata"]["namespace"] = namespace
			} else {
				r.Status["metadata"]["namespace"] = true
			}
		} else {
			r.Status["metadata"]["name"] = namespace
		}
	} else {
		r.Status["info"] = make(map[string]interface{})
		r.Status["info"]["namespace"] = namespace
	}
	r.Dummy["abcde"] = "1234"
	buf, err := json.Marshal(r)
	if err != nil {
		t.Errorf("Failed: CreateRuntimeJsonFile, Marshal, r=%v, err=%v", r, err)
		return nil, err
	}
	return buf, nil
}

func CreateRuntimeJsonFile(t *testing.T, filePath string, namespace string, malform int) error {
	buf, err := GetRuntimeJsonBuf(t, namespace, malform)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Errorf("Failed: CreateRuntimeJsonFile, OpenFile, filePath=%v, err=%v", filePath, err)
		return err
	}
	defer f.Close()
	count := 0
	for count < len(buf) {
		c, err := f.Write(buf[count:])
		if err != nil {
			t.Errorf("Failed: CreateRuntimeJsonFile, Write, filePath=%v, count=%v, len(buf)=%v, err=%v", filePath, count, len(buf), err)
			return err
		}
		count += c
	}
	return nil
}

func CreateZipFile(t *testing.T, filePath string, namespace string, malform int) error {
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Errorf("Failed: CreateZipFile, OpenFile, filePath=%v, err=%v", filePath, err)
		return err
	}
	zw := zip.NewWriter(f)
	defer func() {
		zw.Flush()
		zw.Close()
		f.Close()
	}()

	buf, err := GetRuntimeJsonBuf(t, namespace, malform)
	if err != nil {
		return err
	}

	if malform < 3 {
		w, err := zw.Create("abcdefg-runtime-info.json")
		if err != nil {
			t.Errorf("Failed: CreateZipFile, Create, err=%v", err)
			return err
		}
		bw := bufio.NewWriter(w)
		bw.WriteString(string(buf))
		if err := bw.Flush(); err != nil {
			t.Errorf("Failed: CreateZipFile, Flush, err=%v", err)
			return err
		}
	}

	buf2 := randString(1024)
	w, err := zw.Create("00000.txt")
	if err != nil {
		t.Errorf("Failed: CreateZipFile, Create, err=%v", err)
		return err
	}
	bw := bufio.NewWriter(w)
	bw.WriteString(string(buf2))
	if err := bw.Flush(); err != nil {
		t.Errorf("Failed: CreateZipFile, Flush, err=%v", err)
		return err
	}
	w, err = zw.Create("zzzzzzz.json")
	if err != nil {
		t.Errorf("Failed: CreateZipFile, Create, err=%v", err)
		return err
	}
	bw = bufio.NewWriter(w)
	bw.WriteString(string(buf))
	if err := bw.Flush(); err != nil {
		t.Errorf("Failed: CreateZipFile, Flush, err=%v", err)
		return err
	}
	return nil
}

func TestIsValidSuffix(t *testing.T) {
	tmpDir := t.TempDir()
	testFileName := filepath.Join(tmpDir, "a.zip")
	err := CreateRandomFile(t, testFileName, 4)
	if err != nil {
		return
	}
	testFileName2 := filepath.Join(tmpDir, "a.txt")
	err = CreateRandomFile(t, testFileName2, 4)
	if err != nil {
		return
	}
	z := NewZippedCoreDump("default")
	assert.Equal(t, true, z.IsValidFile(testFileName))
	assert.NotEqual(t, nil, z.IsValidFile("a.txt"))
	assert.NotEqual(t, nil, z.IsValidFile("b.txt"))
}

func TestBegin(t *testing.T) {
	testFileName := "a.zip"
	err := CreateRandomFile(t, testFileName, 4)
	if err != nil {
		return
	}
	z := NewZippedCoreDump("default")
	assert.Equal(t, nil, z.Begin(testFileName))
	z.End()
	_, err = os.Stat(testFileName)
	assert.Equal(t, true, os.IsNotExist(err), "File must be deleted after completion, testFileName=%v", testFileName)
	assert.NotEqual(t, nil, z.Begin("b.zip"))
}

func TestParseRuntimeJsonBuf(t *testing.T) {
	testNamespace := "TestParseRuntimeJsonBuf"
	buf, err := GetRuntimeJsonBuf(t, testNamespace, -1)
	if err != nil {
		return
	}
	buf2, err := GetRuntimeJsonBuf(t, testNamespace, 0)
	if err != nil {
		return
	}
	buf3, err := GetRuntimeJsonBuf(t, testNamespace, 1)
	if err != nil {
		return
	}
	buf4, err := GetRuntimeJsonBuf(t, testNamespace, 2)
	if err != nil {
		return
	}
	buf5 := randString(16)
	z := NewZippedCoreDump(testNamespace)
	namespace, err := z.ParseRuntimeJsonBuf(buf)
	if err != nil {
		t.Errorf("Failed: ParseRutnimeJsonBuf, buf=%v, err=%v", buf, err)
		return
	}
	assert.Equal(t, testNamespace, namespace)
	_, err = z.ParseRuntimeJsonBuf(buf2)
	assert.NotEqual(t, nil, err)
	_, err = z.ParseRuntimeJsonBuf(buf3)
	assert.NotEqual(t, nil, err)
	_, err = z.ParseRuntimeJsonBuf(buf4)
	assert.NotEqual(t, nil, err)
	_, err = z.ParseRuntimeJsonBuf(buf5)
	assert.NotEqual(t, nil, err)
}

func TestExtractRuntimeJson(t *testing.T) {
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "a.zip")
	err := CreateZipFile(t, testFilePath, "default", -1)
	if err != nil {
		return
	}
	malformTestFilePath := filepath.Join(tmpDir, "b.zip")
	err = CreateZipFile(t, malformTestFilePath, "default", 3)
	if err != nil {
		return
	}
	z := NewZippedCoreDump("default")
	buf, err := z.ExtractRuntimeJson(testFilePath)
	assert.Equal(t, nil, err)
	assert.NotEqual(t, nil, buf)
	buf, err = z.ExtractRuntimeJson(malformTestFilePath)
	assert.NotEqual(t, nil, err)
	assert.Equal(t, []byte(nil), buf)
	buf, err = z.ExtractRuntimeJson("c.zip")
	assert.NotEqual(t, nil, err)
	assert.Equal(t, []byte(nil), buf)
}

func TestGetNamespace(t *testing.T) {
	defaultNamespace := "deafult"
	testNamespace := "test"
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "a.zip")
	err := CreateZipFile(t, testFilePath, testNamespace, -1)
	if err != nil {
		return
	}
	malforms := make([]string, 0)
	for i := 2; i <= 3; i++ {
		malformedFilePath := filepath.Join(tmpDir, fmt.Sprintf("a-%d.zip", i))
		err := CreateZipFile(t, malformedFilePath, testNamespace, i)
		if err != nil {
			return
		}
		malforms = append(malforms, malformedFilePath)
	}
	z := NewZippedCoreDump(defaultNamespace)
	assert.Equal(t, defaultNamespace, z.GetNamespace())
	err = z.Begin(testFilePath)
	if err != nil {
		t.Errorf("Failed: TestGetNamespace, Begin, testFilePath=%v, err=%v", testFilePath, err)
		return
	}
	assert.Equal(t, testNamespace, z.GetNamespace())
	z.End()
	for _, malformedFilePath := range malforms {
		err = z.Begin(malformedFilePath)
		if err != nil {
			t.Errorf("Failed: TestGetNamespace, Begin, malformedFilePath=%v, err=%v", malformedFilePath, err)
			return
		}
		assert.NotEqual(t, testNamespace, z.GetNamespace())
		z.End()
	}
}

type ZippedCoreDumpNoDelete struct {
	z ZippedCoreDump
}

func NewZippedCoreDumpNoDelete(defaultNamespace string) *ZippedCoreDumpNoDelete {
	return &ZippedCoreDumpNoDelete{z: NewZippedCoreDump(defaultNamespace)}
}
func (z *ZippedCoreDumpNoDelete) IsValidFile(filePath string) bool {
	return z.z.IsValidFile(filePath)
}
func (z *ZippedCoreDumpNoDelete) Begin(filePath string) error {
	return z.z.Begin(filePath)
}
func (z *ZippedCoreDumpNoDelete) End() {
	f := z.GetFile()
	if f != nil {
		if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
			log.Printf("WARN: Flock (LOCK_UN), filePath=%v, err=%v", f.Name(), err)
		}
		if err := f.Close(); err != nil {
			log.Printf("WARN: Close, filePath=%v, err=%v", f.Name(), err)
		}
	}
}
func (z *ZippedCoreDumpNoDelete) ParseRuntimeJsonBuf(buf []byte) (namespace string, err error) {
	return z.z.ParseRuntimeJsonBuf(buf)
}
func (z *ZippedCoreDumpNoDelete) ExtractRuntimeJson(filePath string) ([]byte, error) {
	return z.z.ExtractRuntimeJson(filePath)
}
func (z *ZippedCoreDumpNoDelete) GetNamespace() (namespace string) {
	return z.z.GetNamespace()
}
func (z *ZippedCoreDumpNoDelete) GetFile() *os.File {
	return z.z.GetFile()
}
