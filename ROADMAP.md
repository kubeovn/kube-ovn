# Kube-OVN RoadMap

This document defines high level goals for Kube-OVN project. We welcome community contributors to discuss and update this Roadmap through Issues.

## Network Datapath

Kube-OVN currently supports two network modes, Overlay and Underlay. We hope to improve the stability, performance, and compatibility with the ecosystem of these two network modes in Kubernetes.

-  Improved Datapath network performance
-  Keeping up with the latest network API features in the community
-  Enhanced network monitoring and visualization capabilities
-  Addition of automated test cases for various scenarios

## VPC Network

VPC network is a key feature of Kube-OVN, many functions have been used in production environment, and we hope to increase the maturity of these functions and improve the user experiences.

-  Standardize multiple gateway solutions and provide the best egress practice
-  Provide more VPC internal basic network capabilities and solutions, such as DNS, DHCP, LoadBalancer, etc.
-  Simplify VPC operation complexity and provide a more comprehensive CLI
-  Supplement automated test cases for various scenarios

## User Experience

Improve the user experience of Kubernetes cni, making container networking simpler, more reliable, and efficient.

- Helm/Operator to automate daily operations
- More organized metrics and grafana dashboard
- Troubleshooting tools that can automatically find known issues
- Integrated with other projects like kubeaz, kubekey, sealos etc.
