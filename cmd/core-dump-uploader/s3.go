/*
 * Copyright 2023- IBM Inc. All rights reserved
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

type S3Client interface {
	ResetClient(accessKey string, secretKey string, endpoint string) error
	CreateBucket(bucket string) error
	IsBucketExist(bucket string) error
	PutObject(bucket string, keyPrefix string, f *os.File) error
	GetRawClient() *s3.S3
}

type S3ClientImpl struct {
	s *s3.S3
}

func NewS3Client() S3Client {
	return &S3ClientImpl{}
}

func (s *S3ClientImpl) ResetClient(accessKey string, secretKey string, endpoint string) error {
	conf := aws.NewConfig().
		WithCredentials(credentials.NewStaticCredentials(accessKey, secretKey, "")).
		WithEndpoint(endpoint).
		WithRegion("us-east") // dummy region to avoid assert
	session, err := session.NewSession(conf)
	if err != nil {
		return fmt.Errorf("failed: NewS3Client, NewSession: err=%v", err)
	}
	s.s = s3.New(session, conf)
	return nil
}

func GetLocationConstraintString(endpoint string) string {
	rep := regexp.MustCompile(`https://s3\.(direct\.|private\.)*([a-z0-9-]+)\.cloud-object-storage\.appdomain\.cloud`)
	//rep := regexp.MustCompile(`https://s3\.([a-z0-9-]+)\.cloud-object-storage\.appdomain\.cloud`)
	result := rep.FindStringSubmatch(endpoint)
	if len(result) < 2 {
		return ""
	}
	return fmt.Sprintf("%s-smart", result[2])
}

func (s *S3ClientImpl) CreateBucket(bucket string) error {
	constraint := GetLocationConstraintString(s.s.Endpoint)
	_, err := s.s.CreateBucket(&s3.CreateBucketInput{
		Bucket: &bucket,
		CreateBucketConfiguration: &s3.CreateBucketConfiguration{
			LocationConstraint: &constraint,
		},
	})
	awsErr, ok := err.(awserr.Error)
	reqErr, ok2 := err.(awserr.RequestFailure)
	if err != nil && (ok && (awsErr.Code() != s3.ErrCodeBucketAlreadyOwnedByYou && awsErr.Code() != s3.ErrCodeBucketAlreadyExists) || (ok2 && reqErr.StatusCode() != 409)) {
		return fmt.Errorf("failed: CreateBucket: bucket=%v, err=%v", bucket, err)
	}
	log.Printf("INFO: CreateBukcet: bucket=%v", bucket)
	return nil
}

func (s *S3ClientImpl) IsBucketExist(bucket string) error {
	_, err := s.s.HeadBucket(&s3.HeadBucketInput{Bucket: &bucket})
	if err != nil {
		awsErr, ok := err.(awserr.Error)
		reqErr, ok2 := err.(awserr.RequestFailure)
		if (ok && awsErr.Code() == s3.ErrCodeNoSuchBucket) || (ok2 && reqErr.StatusCode() == 404) {
			return os.ErrNotExist
		}
	}
	return err
}

func (s *S3ClientImpl) PutObject(bucket string, keyPrefix string, f *os.File) error {
	key := keyPrefix + filepath.Base(f.Name())
	_, err := s.s.PutObject(&s3.PutObjectInput{
		Body:   f,
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("failed: PutObject: bucket=%v, key=%v, err=%v", bucket, key, err)
	}
	log.Printf("INFO: PutObject: %v->s3://%v/%v", f.Name(), bucket, key)
	return nil
}

func (s *S3ClientImpl) GetRawClient() *s3.S3 {
	return s.s
}
