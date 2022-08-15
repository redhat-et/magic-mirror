FROM fedora:36

RUN curl -L https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/stable/oc-mirror.tar.gz -o /tmp/oc-mirror.tar.gz && tar -xzvf /tmp/oc-mirror.tar.gz -C /usr/bin/ && rm -rf /tmp/oc-mirror.tar.gz 

USER 1001

ENTRYPOINT [ "/bin/bash" ]
