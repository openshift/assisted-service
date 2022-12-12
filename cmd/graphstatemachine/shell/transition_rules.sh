echo "## Transition Rules" >>"${OUT_FILE}"
echo "Transition rules are the rules that define the required source states and conditions needed to move to a particular destination state when a particular transition type happens" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"
jq '.transition_rules[]' -c "${JSON}" | while read -r transition_rule_json; do
    transition_rule_name=$(echo "$transition_rule_json" | jq '.name' -r)
    echo Processing "$transition_rule_name"
    transition_rule_description=$(echo "$transition_rule_json" | jq '.description' -r)
    transition_rule_source_states=$(echo "$transition_rule_json" | jq '.source_states[]' -r)
    transition_rule_destination_state=$(echo "$transition_rule_json" | jq '.destination_state' -r)

    echo "### $transition_rule_name" >>"${OUT_FILE}"
    echo "$transition_rule_description" >>"${OUT_FILE}"
    echo "" >>"${OUT_FILE}"

    echo "#### Source states" >>"${OUT_FILE}"
    for state in $transition_rule_source_states; do
        echo "* $(github_markdown_linkify "$(getStateName "$state")")" >>"${OUT_FILE}"
    done

    echo "" >>"${OUT_FILE}"

    echo "#### Destination state" >>"${OUT_FILE}"
    echo "$(github_markdown_linkify "$(getStateName "$transition_rule_destination_state")")" >>"${OUT_FILE}"
    echo "" >>"${OUT_FILE}"
done

echo "" >>"${OUT_FILE}"
