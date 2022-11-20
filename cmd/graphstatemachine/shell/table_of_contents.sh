echo Generating table of contents...

echo "## Table of Contents" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"
echo "### States" >>"${OUT_FILE}"
for state in $(jq '.states | keys[]' -r "${JSON}"); do
	echo "* $(github_markdown_linkify "$(getStateName $state)")" >>"${OUT_FILE}"
done
echo "" >>"${OUT_FILE}"
echo "### Transition Types" >>"${OUT_FILE}"
echo "Transition types are the events that can cause a state transition" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"
for transition_type in $(jq '.transition_types | keys[]' -r "${JSON}"); do
	echo "* $(github_markdown_linkify "$(getTransitionTypeName "$transition_type")")" >>"${OUT_FILE}"
done
echo "" >>"${OUT_FILE}"
echo "### Transition Rules" >>"${OUT_FILE}"
echo "Transition rules are the rules that define the required source states and conditions needed to move to a particular destination state when a particular transition type happens" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"
jq '.transition_rules[]' -c "${JSON}" | while read -r transition_rule_json; do
	transition_rule_name=$(echo "$transition_rule_json" | jq '.name' -r)
	echo "* $(github_markdown_linkify "$transition_rule_name")" >>"${OUT_FILE}"
done
echo "" >>"${OUT_FILE}"
