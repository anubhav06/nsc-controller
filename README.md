# nsc-controller

A Kubernetes controller & CRD to handle the `NamespaceClass` problem. Find more about the problem [here](https://gist.github.com/jiachengxu/872db564e4261220d63c79adec09da87)

## About
This project is built using [kubebuilder](https://book.kubebuilder.io/quick-start)

It consists to 3 main components:
1. The `NamespaceClass` CRD   
It can be found at [config/crd/bases/akuity.io.my.domain_namespaceclasses.yaml](https://github.com/anubhav06/nsc-controller/blob/main/config/crd/bases/akuity.io.my.domain_namespaceclasses.yaml)
2. The Namespace Controller:  
It can be found at [internal/controller/namespace_controller.go](https://github.com/anubhav06/nsc-controller/blob/main/internal/controller/namespace_controller.go)
3. Example files:  
It can be found at [/examples](https://github.com/anubhav06/nsc-controller/tree/main/examples)

## Installation

1. `git clone https://github.com/anubhav06/nsc-controller.git`
2. `cd nsc-controller`
3. To install the CRD into the cluster: `make install`
4. To run the controller: `make run` (in a seperate terminal)

## Testing

You can use the examples folder for some example resources. 
Note: These are not real policies but just sample policies, to make sure that they are being created/updated/deleted
Once you have done the installation process, follow the below process

1. `kubectl apply -f examples/public-network-custom-resource.yaml`
2. `kubectl apply -f examples/internal-network-custom-resource.yaml`
3. `kubectl apply -f examples/public-network-namespace.yaml`
4. Change the value of `namespaceclass.akuity.io/name` from "public-network" to "internal-network" and try
