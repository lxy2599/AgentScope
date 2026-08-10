# Harbor executes agent commands through bash -lc. This immutable local base
# supplies bash and no task/verifier step performs package installation.
FROM python@sha256:9662417aace5ae7b8e2609cce472b72a8958e134ba372808abe9cc1a0c0125e6
RUN mkdir -p /app && chmod 0777 /app
WORKDIR /app
