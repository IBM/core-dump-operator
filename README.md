# core-dump-operator

An **experimental** operator for https://github.com/IBM/core-dump-handler.
This repository contains a special uploader to enable multi-tenant core-dump collection per namespace.
The custom uploader searches and uses a secret with `type: core-dump-handler` in the namespace that runs a core-dumper process.

## install from commandline

```
make deploy
```

## install as bundle

Build and push the core-dump-uploader
```
make uploader-push
```

Build and push the operator image
```
make docker-build docker-push IMG="myrepo.io/core-dump-operator:v0.0.1"
```

Setup the bundle
```
make bundle IMG="myrepo.io/core-dump-operator:v0.0.1"
```

Build and push the bundle image
```
make bundle-build bundle-push BUNDLE_IMG="myrepo.io/core-dump-operator-bundle:v0.0.1"
```

Run bundle command
```
operator-sdk run bundle myrepo.io/core-dump-operator-bundle:v0.0.1 --pull-secret-name mysecret --namespace target
```

## deploy the deamonset

Update the S3 values in the `config/samples/secrets.yaml`
```
kubectl apply -f config/samples/secrets.yaml \
              -f config/samples/charts_v1alpha1_coredumphandler.yaml
```