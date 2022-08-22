FROM fedora:36

RUN dnf -y install s3fs-fuse rsync unzip wget && dnf clean all

RUN curl "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o "awscliv2.zip" && unzip awscliv2.zip && ./aws/install && rm -rf awscliv2.zip && curl "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/latest/openshift-client-linux.tar.gz" -o oc.tar.gz && tar -xzvf oc.tar.gz -C /usr/bin/

ADD sync.sh /usr/local/bin

ENTRYPOINT [ "/bin/bash" ]
