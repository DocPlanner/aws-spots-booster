#### The following is an example to make use of the AWS IAM OIDC with the AWS Spot Booster in an EKS cluster.

- Ref IAM: https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/cloudprovider/aws/README.md#IAM-Policy
- Ref guide: https://github.com/kubernetes/autoscaler/blob/master/cluster-autoscaler/cloudprovider/aws/CA_with_AWS_IAM_OIDC.md

#### Prerequisites

- An Active EKS cluster (1.14 preferred since it is the latest) against which the user is able to run kubectl commands.
- Cluster must consist of at least one worker node ASG.

A) Create an IAM OIDC identity provider for your cluster with the AWS Management Console using the [documentation] .

B) Create a test [IAM policy] for your service accounts.

```sh
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:GetObject"
      ],
      "Resource": [
        "arn:aws:s3:::my-pod-secrets-bucket/*"
      ]
    }
  ]
}
```

C) Create an IAM role for your service accounts in the console.
- Retrieve the OIDC issuer URL from the Amazon EKS console description of your cluster . It will look something identical to:
  'https://oidc.eks.us-east-1.amazonaws.com/id/xxxxxxxxxx'
- While creating a new IAM role, In the "Select type of trusted entity" section, choose "Web identity".
- In the "Choose a web identity provider" section:
  For Identity provider, choose the URL for your cluster.
  For Audience, type sts.amazonaws.com.

- In the "Attach Policy" section, select the policy to use for your service account, that you created in Section B above.
- After the role is created, choose the role in the console to open it for editing.
- Choose the "Trust relationships" tab, and then choose "Edit trust relationship".
  Edit the OIDC provider suffix and change it from :aud to :sub.
  Replace sts.amazonaws.com to your service account ID.
- Update trust policy to finish.

D) Set up [AWS Spots Booster] using the [tutorial] .

__NOTE:__ Please see [the README](README.md#IAM-Policy) for more information on best practices with this IAM role.

- Create an IAM Policy for aws spots booster with the following permissions

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "autoscaling:DescribeTags"
            ],
            "Resource": ["*"]
        },
        {
            "Effect": "Allow",
            "Action": [
                "autoscaling:SetDesiredCapacity",
                "autoscaling:TerminateInstanceInAutoScalingGroup"
            ],
            "Resource": ["*"]
        }
    ]
}
```

- Attach the above created policy to the *instance role* that's attached to your Amazon EKS worker nodes.
- Download a deployment example file provided by the AWS Spots Booster project on GitHub, run the following command:

```sh
$ wget https://raw.githubusercontent.com/docplanner/aws-spots-booster/main/docs/examples/aws-spots-booster-deployment.yaml
```

- Open the downloaded YAML file in an editor.

##### Change 1:

Set the environment variable (us-east-1) based on the following example.

```sh
    spec:
      serviceAccountName: aws-spots-booster
      containers:
        - image: docplanner/aws-spots-booster:v0.0.1
          name: aws-spots-booster
          resources:
            limits:
              cpu: 100m
              memory: 300Mi
            requests:
              cpu: 100m
              memory: 300Mi
          command:
            - ./manager
            - --zap-log-level=debug
            - --max-concurrent-drains=5
            - --time-between-drains=60s
            - --extra-nodes-over-calculation=3
          env:
            - name: AWS_REGION
              value: <<us-east-1>>
```

##### Change 2:

To use IAM with OIDC, you will have to make the below changes to the file as well.

```sh
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::xxxxx:role/Amazon_CA_role   # Add the IAM role created in the above C section.
  name: aws-spots-booster
  namespace: kube-system
```

- Following this setup, you can test if the aws-spots-booster kicked in and if the role was attached using the below commands:

```sh
$ kubectl get pods -n kube-system
$ kubectl exec -n kube-system aws-spots-booster-xxxxxx-xxxxx  env | grep AWS
```

Output of the exec command should ideally display the values for AWS_REGION, AWS_ROLE_ARN and AWS_WEB_IDENTITY_TOKEN_FILE where the role arn must be the same as the role provided in the service account annotations.

The aws spots booster scaling the worker nodes can also be tested:

[//]: # (TODO: CREATE AN ARTIFICIAL REBALANCERECOMMENDATION)

[//]: #

[AWS Spots Booster]: <https://github.com/docplanner/aws-spots-booster/blob/main/docs/examples/aws-spots-booster-deployment.yaml>
[IAM OIDC]: <https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html>
[IAM policy]: <https://docs.aws.amazon.com/eks/latest/userguide/create-service-account-iam-policy-and-role.html>
[documentation]: <https://docs.aws.amazon.com/eks/latest/userguide/enable-iam-roles-for-service-accounts.html>
[tutorial]: <https://aws.amazon.com/premiumsupport/knowledge-center/eks-cluster-autoscaler-setup/>

   
   
  
