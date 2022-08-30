#!/bin/bash

set -e -o pipefail

if [ $SOURCETYPE == 'AWS' ]; then
    echo "Syncing from AWS"
    until aws s3 ls ${SOURCE}; do
      echo "Cannot access ${SOURCE} sleeping and requeuing..."
      sleep 30;
    done
    aws s3 sync s3://${SOURCE} /data
elif [ $SOURCETYPE == 'HTTP' ]; then
    until $(curl --output /dev/null --silent --head --fail ${SOURCE}; do
        printf '.'
        sleep 5
    done
    echo "Syncing from HTTP"
    wget -r -np -nH -P /data ${SOURCE} -R "index.html*" --exclude-directories "icons"
    chmod 666 -R /data
    exit 0
elif [ $SOURCETYPE == 'SSH' ]; then
    echo "Syncing from SSH"
    rsync -avz -e "ssh -i ${{SSH_KEY}}" ${SOURCE} /data
fi
