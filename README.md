# core-dump-operator

An **experimental** operator for https://github.com/IBM/core-dump-handler

## install with operator-sdk

Run the operator SDK Install 
```
operator-sdk olm install [--olm-namespace=openshift-operator-lifecycle-manager]
```

## install from commandline

```
make deploy
```

## install as bundle

```
operator-sdk run bundle quay.io/number9/core-dump-operator-bundle:v0.0.1 [--olm-namespace=openshift-operator-lifecycle-manager]
```

## deploy the deamonset

Update the S3 values in the `config/samples/charts_v1alpha1_coredumphandler.yaml`

```
kubectl apply -f config/samples/charts_v1alpha1_coredumphandler.yaml
```