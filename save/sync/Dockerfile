FROM fedora:36

RUN dnf -y install s3fs-fuse rsync && dnf clean all

USER 1001

ENTRYPOINT [ "/bin/bash" ]
