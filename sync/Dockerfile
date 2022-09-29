FROM fedora:36

RUN dnf -y install s3fs-fuse rsync unzip wget && dnf clean all

RUN curl -L -o s5cmd_2.0.0_Linux-64bit.tar.gz https://github.com/peak/s5cmd/releases/download/v2.0.0/s5cmd_2.0.0_Linux-64bit.tar.gz && tar -xzvf s5cmd_2.0.0_Linux-64bit.tar.gz -C /usr/bin/

ADD import.sh /usr/local/bin
ADD export.sh /usr/local/bin

ENTRYPOINT [ "/bin/bash" ]
