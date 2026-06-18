#!/bin/bash

{{ if .OverwriteNtpConfig }}
# Replace mode: use only the specified NTP sources, no default pool
cat > /etc/chrony.conf <<.
{{ range .DesiredNtpSources }}server {{ . }} iburst
{{ end }}driftfile /var/lib/chrony/drift
makestep 1.0 -1
rtcsync
logdir /var/log/chrony
.
{{ else }}
# Append mode: add sources to existing chrony.conf
# Allow chrony to step the clock at any time if the offset is larger than 1 second.
# The default "makestep 1.0 3" only allows stepping during the first 3 clock updates,
# which may have been exhausted before valid NTP sources were available.
# Using -1 removes this limit, ensuring the clock can be corrected even with large offsets.
sed -i 's/^makestep .*/makestep 1.0 -1/' /etc/chrony.conf

cat >> /etc/chrony.conf <<.
{{ range .DesiredNtpSources }}
server {{ . }} iburst
{{ end }}
.
{{ end }}
