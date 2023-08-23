/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

const testUploadYamlFile = "../../secret.yaml"

func NewCoreDumpUploaderSecretFromFile(fileName string) (*CoreDumpUploaderSecret, error) {
	f, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var ret CoreDumpUploaderSecret
	if err := yaml.Unmarshal(buf, &ret); err != nil {
		return nil, err
	}
	log.Printf("INFO: NewCoreDumpUploaderSecretFromFile, fileName=%v, bucket=%v, keyPrefix=%v, endpoint=%v, createBucket=%v",
		fileName, ret.Bucket, ret.KeyPrefix, ret.Endpoint, ret.CreateBucket)
	return &ret, nil
}

func GetS3Client(t *testing.T, testUploadYamlFile string) (S3Client, error) {
	if _, err := os.Stat(testUploadYamlFile); err != nil {
		t.SkipNow()
		return nil, err
	}
	c, err := NewCoreDumpUploaderSecretFromFile(testUploadYamlFile)
	if err != nil {
		t.Errorf("Failed: NewCoreDumpUploaderSecretFromFile, file=%v", testUploadYamlFile)
		return nil, err
	}
	s := NewS3Client()
	err = s.ResetClient(c.AccessKey, c.SecretKey, c.Endpoint)
	if err != nil {
		t.Errorf("Failed: ResetClient, file=%v", testUploadYamlFile)
		return nil, err
	}
	return s, nil
}

func TestGetLocationConstraintString(t *testing.T) {
	endpointTypes := []string{"direct", "private"}
	regions := []string{
		"us", "us-east", "us-south", "eu", "eu-gb", "eu-de",
		"ap", "jp-tok", "jp-osa", "au-syd", "ca-tor",
		"ams03", "che01", "mex01", "mil01", "mon01", "par01", "sjc04", "sao01", "sng01",
	}
	for _, region := range regions {
		endpoint := fmt.Sprintf("https://s3.%s.cloud-object-storage.appdomain.cloud", region)
		assert.Equal(t, fmt.Sprintf("%s-smart", region), GetLocationConstraintString(endpoint))
	}
	for _, endpointType := range endpointTypes {
		for _, region := range regions {
			endpoint := fmt.Sprintf("https://s3.%s.%s.cloud-object-storage.appdomain.cloud", endpointType, region)
			assert.Equal(t, fmt.Sprintf("%s-smart", region), GetLocationConstraintString(endpoint))
		}
	}

	assert.Equal(t, "", GetLocationConstraintString("https://s3.us-east.ibm.com"))
	assert.Equal(t, "", GetLocationConstraintString("https://s3.indirect.us.cloud-object-storage.appdomain.cloud"))
}

func TestCreateBucket(t *testing.T) {
	s, err := GetS3Client(t, testUploadYamlFile)
	if err != nil {
		return
	}

	bucketName := "tyos-core-dump-handler-test-bucket-ops"
	err = s.CreateBucket(bucketName)
	if err != nil {
		t.Errorf("Failed: CreateBucket, file=%v, err=%v", testUploadYamlFile, err)
		return
	}
	if err := s.IsBucketExist(bucketName); err != nil {
		t.Errorf("Failed: IsBucketExist, file=%v, err=%v", testUploadYamlFile, err)
		return
	}
	_, err = s.GetRawClient().DeleteBucket(&s3.DeleteBucketInput{Bucket: &bucketName})
	awsErr, ok := err.(awserr.Error)
	reqErr, ok2 := err.(awserr.RequestFailure)
	if err != nil && ((!ok || awsErr.Code() != s3.ErrCodeNoSuchBucket) || (!ok2 || reqErr.StatusCode() != 404)) {
		t.Errorf("Failed: DeleteBucket, file=%v, err=%v", testUploadYamlFile, err)
		return
	}
}

func TestCreateExistBucket(t *testing.T) {
	s, err := GetS3Client(t, testUploadYamlFile)
	if err != nil {
		return
	}

	bucketName := "tyos-core-dump-handler-test-put-object"
	err = s.CreateBucket(bucketName)
	if err != nil {
		t.Errorf("Failed: CreateBucket, file=%v, err=%v", testUploadYamlFile, err)
		return
	}
	assert.Equal(t, nil, err) // ignore recreation
}

func TestPutObject(t *testing.T) {
	s, err := GetS3Client(t, testUploadYamlFile)
	if err != nil {
		return
	}

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "TestUpload")
	size := int64(128)
	if err := CreateRandomFile(t, filePath, size); err != nil {
		return
	}

	bucketName := "tyos-core-dump-handler-test-put-object"
	keyPrefix := "tyos/"

	f, err := os.Open(filePath)
	if err != nil {
		t.Errorf("Failed: Open, filePath=%v, err=%v", filePath, err)
		return
	}
	defer f.Close()

	err = s.PutObject(bucketName, keyPrefix, f)
	if err != nil {
		t.Errorf("Failed: PutObject, bucketName=%v, keyPrefix=%v, f.Name()=%v, err=%v", bucketName, keyPrefix, f.Name(), err)
		return
	}
	key := keyPrefix + filepath.Base(filePath)
	ret, err := s.GetRawClient().HeadObject(&s3.HeadObjectInput{Bucket: &bucketName, Key: &key})
	if err != nil {
		t.Errorf("Failed: HeadObject, key=%v, err=%v", key, err)
		return
	}
	assert.Equal(t, size, *ret.ContentLength, "Failed: HeadObject returns incorrect content-length")
}

type MockS3Client struct {
	resetClientFail   error
	createBucketFail  error
	isBucketExistFail error
	putObjectFail     error
}

func NewMockS3Client(resetClientFail error, createBucketFail error, isBucketExistFail error, putObjectFail error) *MockS3Client {
	return &MockS3Client{
		resetClientFail: resetClientFail, createBucketFail: createBucketFail,
		isBucketExistFail: isBucketExistFail, putObjectFail: putObjectFail,
	}
}

func (s *MockS3Client) ResetClient(accessKey string, secretKey string, endpoint string) error {
	return s.resetClientFail
}

func (s *MockS3Client) CreateBucket(string) error {
	return s.createBucketFail
}

func (s *MockS3Client) IsBucketExist(string) error {
	return s.isBucketExistFail
}

func (s *MockS3Client) PutObject(string, string, *os.File) error {
	return s.putObjectFail
}

func (s *MockS3Client) GetRawClient() *s3.S3 {
	return nil
}
