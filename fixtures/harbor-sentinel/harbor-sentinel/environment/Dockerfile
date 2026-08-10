# Harbor's Docker environment executes agent commands through `bash -lc`; use
# the locally pinned Python base that already provides bash. No package manager
# or network access occurs while the task or verifier runs.
FROM python@sha256:9662417aace5ae7b8e2609cce472b72a8958e134ba372808abe9cc1a0c0125e6
RUN mkdir -p /app && chmod 0777 /app
WORKDIR /app
