/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/unix"
)

func TestNewCoreDumpUploaderSecret(t *testing.T) {
	expected := map[string][]byte{
		"bucket":       []byte("bucket"),
		"keyPrefix":    []byte("a/b/c"),
		"accessKey":    []byte("ABCDEF"),
		"secretKey":    []byte("12345"),
		"endpoint":     []byte("https://endpoint.io"),
		"createBucket": []byte("true"),
	}
	c, err := NewCoreDumpUploaderSecret(expected)
	if err != nil {
		t.Errorf("Failed: NewCoreDumpUploaderSecret, expected=%v, err=%v", expected, err)
		return
	}
	assert.Equal(t, string(expected["accessKey"]), c.AccessKey)
	assert.Equal(t, string(expected["secretKey"]), c.SecretKey)
	assert.Equal(t, string(expected["bucket"]), c.Bucket)
	assert.Equal(t, string(expected["endpoint"]), c.Endpoint)
	assert.Equal(t, string(expected["keyPrefix"]), c.KeyPrefix)

	malformed := map[string][]byte{
		"bucket":       []byte("bucket"),
		"keyPrefix":    []byte("a/b/c"),
		"accessKey":    []byte("ABCDEF"),
		"secretKey":    []byte("12345"),
		"endPoint":     []byte("https://endpoint.io"),
		"createBucket": []byte("createBucket"),
	}
	_, err = NewCoreDumpUploaderSecret(malformed)
	assert.NotEqual(t, nil, err)
	malformed = map[string][]byte{
		"bucket":       []byte("bucket"),
		"keyPrefix":    []byte("a/b/c"),
		"secretKey":    []byte("12345"),
		"endPoint":     []byte("https://endpoint.io"),
		"createBucket": []byte("true"),
	}
	_, err = NewCoreDumpUploaderSecret(malformed)
	assert.NotEqual(t, nil, err)
}

func TestProcessSingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "a.zip")
	filePath2 := filepath.Join(tmpDir, "a.txt")
	err := CreateZipFile(t, filePath, "default", -1)
	if err != nil {
		return
	}
	err = CreateRandomFile(t, filePath2, 4)
	if err != nil {
		return
	}
	zip := NewZippedCoreDumpNoDelete("default")
	s3 := NewMockS3Client(nil, nil, nil, nil)
	k8s := NewMockK8sClient(unix.EINVAL, nil, nil, false, false)
	assert.Equal(t, unix.EINVAL, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
	k8s = NewMockK8sClient(nil, nil, unix.ENOENT, false, false)
	assert.Equal(t, unix.ENOENT, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
	k8s = NewMockK8sClient(nil, nil, nil, true, false)
	assert.NotEqual(t, nil, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
	k8s = NewMockK8sClient(nil, nil, nil, false, false)

	s3 = NewMockS3Client(unix.EINVAL, nil, nil, nil)
	assert.NotEqual(t, nil, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
	s3 = NewMockS3Client(nil, nil, unix.EIO, nil)
	assert.NotEqual(t, nil, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
	s3 = NewMockS3Client(nil, nil, nil, unix.EACCES)
	assert.NotEqual(t, nil, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))

	k8s = NewMockK8sClient(nil, nil, nil, false, true)
	s3 = NewMockS3Client(nil, os.ErrNotExist, os.ErrNotExist, nil)
	assert.NotEqual(t, nil, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))

	s3 = NewMockS3Client(nil, nil, nil, nil)
	u := NewUploader(zip, k8s, s3)
	assert.Equal(t, nil, u.ProcessSingleFile(filePath))
	assert.Equal(t, nil, u.ProcessSingleFile(filePath2))
	assert.Equal(t, nil, u.ProcessSingleFile(filepath.Join(tmpDir, "b.zip")))

	k8s = NewMockK8sClient(nil, unix.EIO, nil, false, false)
	assert.Equal(t, unix.EIO, NewUploader(zip, k8s, s3).ProcessSingleFile(filePath))
}

func TestRun(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "a.zip")
	go func() {
		time.Sleep(time.Second)
		if err := CreateRuntimeJsonFile(t, filePath, "default", 2); err != nil {
			t.Errorf("Failed: CopyZipInput, tmpDir=%v, err=%v", tmpDir, err)
			return
		}
	}()
	go func() {
		zip := NewZippedCoreDump("default")
		k8s := NewMockK8sClient(nil, nil, nil, false, false)
		s3 := NewMockS3Client(nil, nil, nil, nil)
		NewUploader(zip, k8s, s3).Run(tmpDir)
	}()
	time.Sleep(time.Second)
	var ok = false
	for begin := time.Now(); time.Since(begin).Seconds() < 3; {
		time.Sleep(time.Second)
		dirs, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Errorf("Failed: TestUpload, ReadDir, tmpDir=%v, err=%v", tmpDir, err)
		}
		// check if WriteNotifier removed files
		if len(dirs) == 0 {
			ok = true
			break
		}
	}
	assert.Equal(t, true, ok)
}

func TestGetVersion(t *testing.T) {
	verStr := GetVersion()
	assert.NotEqual(t, "", verStr)
}
