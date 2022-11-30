echo "# $(cat "${JSON}" | jq -r '.name') state machine" >"${OUT_FILE}"

echo "$(cat "${JSON}" | jq -r '.description')" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"

