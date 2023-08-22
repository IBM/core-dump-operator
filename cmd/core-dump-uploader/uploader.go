package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
	"gopkg.in/fsnotify.v1"
)

type CoreDumpUploaderSecret struct {
	Bucket       string `yaml:"bucket"`
	KeyPrefix    string `yaml:"keyPrefix"`
	AccessKey    string `yaml:"accessKey"`
	SecretKey    string `yaml:"secretKey"`
	Endpoint     string `yaml:"endpoint"`
	CreateBucket bool   `yaml:"createBucket"`
}

func NewCoreDumpUploaderSecret(data map[string][]byte) (*CoreDumpUploaderSecret, error) {
	noEnt := make([]string, 0)
	for _, ent := range []string{"bucket", "keyPrefix", "accessKey", "secretKey", "endpoint", "createBucket"} {
		if _, ok := data[ent]; !ok {
			noEnt = append(noEnt, ent)
		}
	}
	if len(noEnt) == 0 {
		createBucket, err := strconv.ParseBool(string(data["createBucket"]))
		if err != nil {
			return nil, fmt.Errorf("failed: NewCoreDumpUploaderSecret, malformed core-dump-handler secret, cannot parse bool createBucket, %v", data["createBucket"])
		}
		return &CoreDumpUploaderSecret{
			Bucket: string(data["bucket"]), KeyPrefix: string(data["keyPrefix"]),
			AccessKey: string(data["accessKey"]), SecretKey: string(data["secretKey"]), Endpoint: string(data["endpoint"]),
			CreateBucket: createBucket,
		}, nil
	}
	return nil, fmt.Errorf("failed: NewCoreDumpUploaderSecret, malformed core-dump-handler secret, missing entries=%v", strings.Join(noEnt, ","))
}

type Uploader struct {
	zip       ZippedCoreDump
	k8sClient K8sClient
	s3Client  S3Client
}

func NewUploader(zip ZippedCoreDump, k8sClient K8sClient, s3Client S3Client) *Uploader {
	return &Uploader{zip: zip, k8sClient: k8sClient, s3Client: s3Client}
}

func (u *Uploader) ProcessSingleFile(filePath string) error {
	if !u.zip.IsValidFile(filePath) {
		return nil
	}
	if err := u.zip.Begin(filePath); err != nil {
		return err
	}
	defer u.zip.End()

	err := u.k8sClient.ResetClient()
	if err != nil {
		return err
	}
	namespace := u.zip.GetNamespace()
	if err := u.k8sClient.CheckNamespace(namespace); err != nil {
		return err
	}
	secretData, err := u.k8sClient.GetSecret(namespace)
	if err != nil {
		return err
	}
	c, err := NewCoreDumpUploaderSecret(secretData)
	if err != nil {
		return err
	}
	err = u.s3Client.ResetClient(c.AccessKey, c.SecretKey, c.Endpoint)
	if err != nil {
		return err
	}
	if err := u.s3Client.IsBucketExist(c.Bucket); err != nil {
		if c.CreateBucket {
			if err := u.s3Client.CreateBucket(c.Bucket); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if err := u.s3Client.PutObject(c.Bucket, filepath.Join(c.KeyPrefix, namespace)+"/", u.zip.GetFile()); err != nil {
		return err
	}
	return nil
}

func (u *Uploader) Run(watchDir string) error {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, unix.SIGTERM, unix.SIGINT)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("NewWatcher, err=%v", err)
	}
	defer watcher.Close()
	watcher.Add(watchDir)

	var stopped = false
	for !stopped {
		select {
		case event := <-watcher.Events:
			switch event.Op {
			case fsnotify.Write:
				if err := u.ProcessSingleFile(event.Name); err != nil {
					log.Printf("%v", err)
				}
			default:
				// ignore other events
			}
		case signal := <-signalChan:
			log.Printf("Received Signal: %v", signal.String())
			stopped = true
		case err := <-watcher.Errors:
			return fmt.Errorf("watcher.Errors, watchDir=%v, err=%v", watchDir, err)
		}
	}
	return nil
}

func GetVersion() (ret string) {
	info, ok := debug.ReadBuildInfo()
	if ok {
		rev := "unknown"
		ts := "unknown"
		for _, kv := range info.Settings {
			switch kv.Key {
			case "vcs.revision":
				rev = kv.Value
			case "vcs.time":
				ts = kv.Value
			}
			if rev != "unknown" && ts != "unknown" {
				break
			}
		}
		ret = fmt.Sprintf("Start %v (rev: %v (%v), %v)", filepath.Base(info.Path), rev, ts, info.GoVersion)
	}
	return ret
}

var watchDir, defaultNamespace, namespaceLabelSelector string

func init() {
	flag.StringVar(&watchDir, "watchDir", "/mnt/core-dump-handler/", "Directory path to be watched")
	flag.StringVar(&defaultNamespace, "defaultNamespace", "core-dump-handler", "Default namespace for upload")
	flag.StringVar(&namespaceLabelSelector, "namespaceLabelSelector", "kubernetes.io/metadata.name=core-dump-handler", "Label selector to enable uploads (format: key1=value1,key2=value2)")
}

func main() {
	log.Print(GetVersion())
	flag.Parse()
	k8s := NewK8sClient("", namespaceLabelSelector)
	s3 := NewS3Client()
	zip := NewZippedCoreDump(defaultNamespace)
	NewUploader(zip, k8s, s3).Run(watchDir)
}
