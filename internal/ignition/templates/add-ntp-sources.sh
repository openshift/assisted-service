#!/bin/bash

# Allow chrony to step the clock at any time if the offset is larger than 1 second.
# The default "makestep 1.0 3" only allows stepping during the first 3 clock updates,
# which may have been exhausted before valid NTP sources were available.
# Using -1 removes this limit, ensuring the clock can be corrected even with large offsets.
sed -i 's/^makestep .*/makestep 1.0 -1/' /etc/chrony.conf

cat >> /etc/chrony.conf <<.
{{ range .AdditionalNtpSources }}
server {{ . }} iburst
{{ end }}
.