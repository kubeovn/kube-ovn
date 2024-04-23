name: 🧩 🐞 Bug Report
description: Report a reproducible bug
title: "[BUG] "
labels: ["bug"]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this bug report!

        Do not forget to update the title above to concisely describe the issue.
  - type: input
    id: kube-ovn-version
    attributes:
      label: Kube-OVN Version
      placeholder: ex. v1.12.12 
    validations:
      required: true
  - type: textarea
    id: kubernetes-version 
    attributes:
      label: Kubernetes Version
      description: |
        Output of `kubectl version`
      placeholder: ex. v1.26.1 
    validations:
      required: true
  - type: textarea
    id: os-info 
    attributes:
      label: Operation-system/Kernel Version
      description: |
        Output of `awk -F '=' '/PRETTY_NAME/ { print $2 }' /etc/os-release` and output of `uname -r`
    validations:
      required: true
  - type: textarea
    id: description
    attributes:
      label: Description
      description: |
        Please provide a clear and concise description of what the bug is. Include screenshots if needed. 
    validations:
      required: true
  - type: textarea
    id: repro
    attributes:
      label: Steps To Reproduce
      description: Your bug will get fixed much faster if maintainers can easily reproduce it. Issues without reproduction steps may be immediately closed as not actionable.
      placeholder: |
        1. In this environment...
        2. With this config...
        3. Run '...'
        4. See error...
    validations:
      required: true
  - type: textarea
    id: current-behavior
    attributes:
      label: Current Behavior
    validations:
      required: true 
  - type: textarea
    id: expected-behavior
    attributes:
      label: Expected Behavior
    validations:
      required: true 