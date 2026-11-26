# dfperf driver sidecar: built FROM the exact client image digest under test,
# so dfget always matches the dfdaemon it talks to over the shared socket.
ARG CLIENT_IMAGE
FROM ${CLIENT_IMAGE}
COPY dfbench /opt/dfperf/dfbench
ENTRYPOINT ["/opt/dfperf/dfbench"]
