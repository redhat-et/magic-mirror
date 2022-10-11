FROM fedora:36

RUN dnf -y install rsync unzip wget && dnf clean all

RUN curl -L -o s5cmd_2.0.0_Linux-64bit.tar.gz https://github.com/peak/s5cmd/releases/download/v2.0.0/s5cmd_2.0.0_Linux-64bit.tar.gz && tar -xzvf s5cmd_2.0.0_Linux-64bit.tar.gz -C /usr/bin/ && rm -rf s5cmd_2.0.0_Linux-64bit.tar.gz

RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" && install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

ADD import.sh /usr/local/bin
ADD export.sh /usr/local/bin

ENTRYPOINT [ "/bin/bash" ]
