name: "🧬 💎 Feature Request"
description: Suggest a new feature or improvement
title: "[Feature Request] "
labels: ["feature"]
body:
  - type: markdown
    attributes:
      value: |
        Thanks for taking the time to fill out this feature request!

        Do not forget to update the title above to concisely describe the issue.
  - type: textarea
    id: description
    attributes:
      label: Description
      description: |
        Please provide a clear and concise description of your idea. Consider adding examples, screenshots, and references.
    validations:
      required: true
  - type: textarea
    id: audience
    attributes:
      label: Who will benefit from this feature?
    validations:
      required: false
  - type: textarea
    attributes:
      label: Anything else?
      description: |
        Links? References? Anything that will give us more context!

        Tip: You can attach images or log files by clicking this area to highlight it and then dragging files in.
    validations:
      required: false