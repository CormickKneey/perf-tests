#!/bin/bash

LINT_DIR=/data
MARKDOWNLINT_IMAGE=avtodev/markdown-lint:v1

docker run --rm \
    -v "$(pwd):${LINT_DIR}:ro" \
    ${MARKDOWNLINT_IMAGE} \
    -c "${LINT_DIR}/.markdownlint.yml" /data/**/*.md \
    -i "${LINT_DIR}/CHANGELOG.md" \
    -i "${LINT_DIR}/compose-bench/results" \
    -i "${LINT_DIR}/compose-bench/dashboard/dist" \
    -i "${LINT_DIR}/compose-bench/dashboard/node_modules" \
    -i "${LINT_DIR}/compose-bench/dashboard/public/data"
