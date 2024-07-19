#!/bin/bash

cat >> /etc/chrony.conf <<.
{{ range .AdditionalNtpSources }}
server {{ . }} iburst
{{ end }}
.