# dfperf tools image: fixtures job, runner and compare (static dfbench binary).
ARG BASE_IMAGE
FROM ${BASE_IMAGE}
COPY dfbench /opt/dfperf/dfbench
ENTRYPOINT ["/opt/dfperf/dfbench"]
