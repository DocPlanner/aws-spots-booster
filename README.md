# AWS Spots Booster

<img src="https://github.com/achetronic/aws-spots-booster/raw/main/docs/img/logo.png" width="100">

A controller for Kubernetes that increase AWS ASGs capacity on `RebalanceRecommendation` events, 
and drain cordoned nodes in a controlled way

<p>
  <a href="https://github.com/kubernetes/kubernetes/releases">
    <img src="https://img.shields.io/badge/Kubernetes-%3E%3D%201.18-brightgreen" alt="kubernetes">
  </a>
  <a href="https://golang.org/doc/go1.19">
    <img src="https://img.shields.io/github/go-mod/go-version/aws/aws-node-termination-handler?color=blueviolet" alt="go-version">
  </a>
  <a href="https://opensource.org/licenses/Apache-2.0">
    <img src="https://img.shields.io/badge/License-Apache%202.0-ff69b4.svg" alt="license">
  </a>
</p>

----

## Motivation

Spot instances are good for saving costs but reliability is pretty important on production environments.
Spots are spots, so AWS can reclaim them on any moment, emitting `RebalanceRecommentation` and `SpotInterruption` events.

The `SpotInterruption` event gives you only 2 minutes to react and move the load. However, a `RebalanceRecommendation`
event is emitted some minutes before.

On Kubernetes, this unreliability issues can be driven using 
[AWS Node Termination Handler](https://github.com/aws/aws-node-termination-handler) to cordon and drain the nodes 
on `SpotInterruption` and `RebalanceRecommendation` notices, together with 
[Cluster Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) to restore the capacity,
creating new nodes when pods become `Pending` after draining

There is a gap between the time some capacity is removed and new one is joined to Kubernetes, causing outages and making
spots unusable on real production scenarios with heavy loads.

This controller is the missing piece in the middle, just to **boost your ASGs containing Spot instances on 
`RebalanceRecommendarion` and drain cordoned nodes on a controlled way**, keeping your capacity over the time.

## Requirements on your cluster

- **Cluster Autoscaler:** This controller relies on Cluster Autoscaler's capacity calculations as a starting point
  to estimate how big is the boost needed for the ASGs, so flag `--write-status-configmap` must be set to `true`
    

- **AWS Node Termination Handler:** This controller relies NTH to create Kubernetes events on `RebalanceRecommendation`.
  This way less permissions are needed, because no need to read them from the AWS.

  For doing it, you need to set `enableRebalanceMonitoring` to `true` on its Helm chart, and be sure 
  that `enableRebalanceDraining` is **disabled** (don't worry, this is the default)

## Permissions

AWS Spots Booster require some permissions on the provider side to be able to terminate instances on drain process or
read tags from ASGs. Several ways to do this can be used, but we recommend to use OIDC on EKS clusters

### Using OIDC Federated Authentication

OIDC federated authentication allows your service to assume an IAM role and interact with AWS services without having 
to store credentials as environment variables. For an example of how to use AWS IAM OIDC with the AWS Spots Booster, 
please see [here](docs/permissions_with_aws_iam_oidc.md).

### Using AWS Credentials

**NOTE** The following is not recommended for Kubernetes clusters running on
AWS. If you are using Amazon EKS, consider using [IAM roles for Service
Accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
instead.

For on-premise clusters, you may create an IAM user subject to the above policy
and provide the IAM credentials as environment variables in the AWS Spots Booster deployment manifest.
AWS Spots Booster will use these credentials to authenticate and authorize itself.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-secret
type: Opaque
data:
  aws_access_key_id: BASE64_OF_YOUR_AWS_ACCESS_KEY_ID
  aws_secret_access_key: BASE64_OF_YOUR_AWS_SECRET_ACCESS_KEY
```

Please refer to the [relevant Kubernetes
documentation](https://kubernetes.io/docs/concepts/configuration/secret/#creating-a-secret-manually)
for creating a secret manually.

```yaml
env:
  - name: AWS_ACCESS_KEY_ID
    valueFrom:
      secretKeyRef:
        name: aws-secret
        key: aws_access_key_id
  - name: AWS_SECRET_ACCESS_KEY
    valueFrom:
      secretKeyRef:
        name: aws-secret
        key: aws_secret_access_key
  - name: AWS_REGION
    value: YOUR_AWS_REGION
```

> Shared config is enabled by default, this means that it's possible to provide a whole config file using 
> **AWS_SDK_LOAD_CONFIG** environment variable with several profiles inside. One of them can be selected just setting 
> its name on **AWS_PROFILE**. 
> Even when this is possible, **it's not recommended** to have the credentials of several profiles
> inside all your environments, so our recommendation is to use this feature only when testing the controller from local
> without deploying it into Kubernetes.

## How to deploy

We are working on a public Helm Chart which will be hosted on this repository. Until then, the 
[simplest deployment](docs/examples/simple-deployment.yaml)
example from the documentation can be used instead.

## Flags

There are several flags that can be configured to change the behaviour of the
application. They are described in the following table:

| Name                             | Description                                                                                |           Default           | Example                                          |
|:---------------------------------|:-------------------------------------------------------------------------------------------|:---------------------------:|:-------------------------------------------------|
| `--connection-mode`              | Connect from inside or outside Kubernetes                                                  |          `kubectl`          | `--connection-mode incluster`                    |
| `--kubeconfig`                   | Path to the kubeconfig file                                                                |      `~/.kube/config`       | `--kubeconfig "~/.kube/config"`                  |
| `--dry-run`                      | Skip actual changes                                                                        |           `false`           | `--dry-run true`                                 |
| `--ca-status-namespace`          | Namespace where to look for Cluster Autoscaler's status configmap                          |        `kube-system`        | `--ca-status-namespace "default"`                |
| `--ca-status-name`               | Name of Cluster Autoscaler's status configmap                                              | `cluster-autoscaler-status` | `--ca-status-name "another-cm"`                  |
| `--ignored-autoscaling-groups`   | Comma-separated list of autoscaling-group names to ignore on ASGs boosting                 |              -              | `--ignored-autoscaling-groups "eks-one,eks-two"` |
| `--extra-nodes-over-calculation` | Extra nodes to add to ASGs over calculated ones                                            |             `0`             | `--extra-nodes-over-calculation 3`               |
| `--disable-drain`                | Disable drain-and-destroy process for nodes under risk (not recommended)                   |           `false`           | `--disable-drain true`                           |
| `--drain-timeout`                | Duration to consider a drain as done when not finished                                     |           `120s`            | `--drain-timeout 2m`                             |
| `--max-concurrent-drains`        | Nodes to drain at once                                                                     |             `5`             | `--max-concurrent-drains 7`                      |
| `--time-between-drains`          | Duration between scheduling a drainages batch and the following (when new nodes are ready) |            `60s`            | `--time-between-drains "1m"`                     |
| `--ignore-pods-grace-period`     | Ignore waiting for pod's grace period on termination when draining                         |           `false`           | `--ignore-pods-grace-period true`                |
| `--max-time-consider-new-node`   | Max time to consider a node as new after joined to the cluster                             |           `-10m`            | `--max-time-consider-new-node -20m`              |
| `--metrics-port`                 | Port where metrics web-server will run                                                     |           `2112`            | `--metrics-port 8080`                            |
| `--metrics-host`                 | Host where metrics web-server will run                                                     |          `0.0.0.0`          | `--metrics-host 0.1.0.2`                         |
| `--help`                         | Show this help message                                                                     |              -              | -                                                |

## FAQ

### Why not using Kubebuilder?

This is the first iteration of this project, and we needed to validate the idea. Coding a solution that several
developers can review was faster for us. If you are reading this, that means the idea worked pretty well, 
and a refactor is already on our roadmap using kubebuilder.

## How to release

Each release of this container is done following several steps carefully in order not to break the things for anyone.

1. Test the changes on the code:

    ```console
    make test
    ```

   > A release is not done if this stage fails

2. Define the package information

    ```console
    export VERSION="0.0.1"
    ```

3. Generate and push the Docker image (published on Docker Hub).

    ```console
    make docker-buildx
    ```

## How to collaborate

We are open to external collaborations for this project: improvements, bugfixes, whatever.

For doing it, open an issue to discuss the need of the changes, then:

- Open an issue, to discuss what is needed and the reasons
- Fork the repository
- Make your changes to the code
- Open a PR and wait for review

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
