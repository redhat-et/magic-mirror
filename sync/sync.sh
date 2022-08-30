#!/bin/bash

set -e -o pipefail

if [ $SOURCETYPE == 'AWS' ]; then
    echo "Syncing from AWS"
    aws s3 sync s3://${SOURCE} /data
elif [ $SOURCETYPE == 'HTTP' ]; then
    echo "Syncing from HTTP"
    wget -r -np -nH -P /data ${SOURCE} -R "index.html*" --exclude-directories "icons"
    chmod 666 -R /data
    exit 0
elif [ $SOURCETYPE == 'SSH' ]; then
    echo "Syncing from SSH"
    rsync -avz -e "ssh -i ${{SSH_KEY}}" ${SOURCE} /data
fi
