# AWS Spots Booster

A controller for Kubernetes that increase AWS ASGs capacity on `RebalanceRecommendation` events, 
and drain cordoned nodes in a controlled way

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

This controller is the missing piece in the middle, just to boost your ASGs containing Spot instances on 
`RebalanceRecommendarion` and drain cordoned nodes on a controlled way, keeping your capacity over the time.

## Requirements on your cluster

- **Cluster Autoscaler:** This controller relies on Cluster Autoscaler's capacity calculations as a starting point
  to estimate how big is the boost needed for the ASGs, so flag `--write-status-configmap` must be set to `true`
    

- **AWS Node Termination Handler:** This controller relies NTH to create Kubernetes events on `RebalanceRecommendation`.
  This way less permissions are needed, because no need to read them from the AWS.

  For doing it, you need to set `enableRebalanceMonitoring` to `true` on its Helm chart, and be sure 
  that `enableRebalanceDraining` is **disabled** (don't worry, this is the default)

## How to deploy

TODO

## Flags

There are several flags that can be configured to change the behaviour of the
application. They are described in the following table:

| Name                | Description                                                       |           Default           | Example                         |
|:--------------------|:------------------------------------------------------------------|:---------------------------:|:--------------------------------|
| `--connection-mode` | Connect from inside or outside Kubernetes                         |          `kubectl`          | `--connection-mode incluster`   |
| `--kubeconfig`      | Path to the kubeconfig file                                       |      `~/.kube/config`       | `--kubeconfig "~/.kube/config"` |
| `--namespace`       | Namespace where to look for Cluster Autoscaler's status configmap |        `kube-system`        | `--namespace "default"`         |
| `--configmap`       | Name of Cluster Autoscaler's status configmap                     | `cluster-autoscaler-status` | `--configmap "another-cm"`      |
| `--help`            | Show this help message                                            |              -              | -                               |

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
    export PLATFORM="linux/amd64"
    export VERSION="0.0.1"
    ```

3. Generate and push the Docker image (published on Docker Hub).

    ```console
    make docker-build docker-push
    ```

## How to collaborate

We are open to external collaborations for this project: improvements, bugfixes, whatever.

For doing it, open an issue to discuss the need of the changes, then:

- Open an issue, to discuss what is needed and the reasons
- Fork the repository
- Make your changes to the code
- Open a PR and wait for review
