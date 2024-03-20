Asklepios
==========

Asklepios is an auto-healing service for stateful pods when kubernetes node
is in NotReady state.

Asklepios checks the state of the node and takes the following actions 
when a node goes to the NotReady state so that the pods on the node can 
be moved to other nodes after the kickout time has elapsed.

* Cordon the node to prevent pods from being scheduled.
* Add a node.kubernetes.io/out-of-service=nodeshutdown:NoExecute taint 
  so that pods stuck on the node can be quickly moved to other nodes.

When the node is healthy and ready, it will revert the actions taken 
during kickout after the kickin configuration time has elapsed.

* Uncordon the node to allow pods to be scheduled.
* Remove the node.kubernetes.io/out-of-service=nodeshutdown:NoExecute taint 
  to allow pods to run on the node.

The configurations are as follows.

* sleep: How often to check node health (in seconds, Default: 10 seconds)
* kickout: Time to wait after a node becomes NotReady 
  before running the kickout process (in seconds, Default: 60 seconds)
* kickin: Time to wait before running the kickin process
  after the node becomes Ready (in seconds, Default: 60 seconds)

If you do not want check a node,
add node.kubernetes.io/asklepios=skip:NoExecute taint on the node.
Then, Asklepios will not check the node status.::

    kubectl taint nodes NODE_NAME node.kubernetes.io/asklepios=skip:NoExecute

If you want to check a node again, 
remove node.kubernetes.io/asklepios=skip:NoExecute taint.::

    kubectl taint nodes NODE_NAME node.kubernetes.io/asklepios=skip:NoExecute-
    
Build
-----

To build a container image::

    ./build.sh

Deploy
-------

To deploy asklepios in kubernetes cluster::

    kubectl apply -f k8s

See if asklepios pod is running.::

    kubectl get po -n kube-system -l app=asklepios
    NAME                         READY   STATUS    RESTARTS   AGE
    asklepios-7fd4d69f95-2rz5c   1/1     Running   0          129m


