echo "## States" >>"${OUT_FILE}"
for state in $(jq '.states | keys[]' -r "${JSON}"); do
	echo Processing "$state"
	state_name=$(getStateName "$state")
	state_description=$(getStateDescription "$state")

	echo "### $state_name" >>"${OUT_FILE}"
	echo "$state_description" >>"${OUT_FILE}"
	echo "" >>"${OUT_FILE}"

	echo "#### Transition types where this is the source state" >>"${OUT_FILE}"
	for transition_type in $(getSourceStateTransitionTypes "$state"); do
		echo "* $(github_markdown_linkify "$(getTransitionTypeName "$transition_type")")" >>"${OUT_FILE}"
	done

	echo "" >>"${OUT_FILE}"

	echo "#### Transition types where this is the destination state" >>"${OUT_FILE}"
	for transition_type in $(getDestinationStateTransitionTypes "$state"); do
		echo "* $(github_markdown_linkify "$(getTransitionTypeName "$transition_type")")" >>"${OUT_FILE}"
	done

	echo "" >>"${OUT_FILE}"

	echo "#### Transition rules where this is the source state" >>"${OUT_FILE}"
	jq --arg state "$state" \
        --from-file "${SCRIPT_DIR}/queries/source_state_transition_rules.jq" \
        "${JSON}" |
		smcat \
			--input-type json \
			--output-type svg \
			--direction left-right \
			--engine fdp \
			--output-to "${OUT_DIR}"/media/source_"${state}".svg

	echo "![source_${state}](./media/source_${state}.svg)" >>"${OUT_FILE}"
	echo "" >>"${OUT_FILE}"

	getSourceStateTransitionRules "$state" | while read -r transition_rule; do
		echo "* $(github_markdown_linkify "$transition_rule")" >>"${OUT_FILE}"
	done

	echo "" >>"${OUT_FILE}"

	echo "#### Transition rules where this is the destination state" >>"${OUT_FILE}"
	jq --arg state "$state" \
        --from-file "${SCRIPT_DIR}/queries/dest_state_transition_rules.jq" \
        "${JSON}" |
		smcat \
			--input-type json \
			--output-type svg \
			--direction left-right \
			--engine fdp \
			--output-to "${OUT_DIR}"/media/destination_"${state}".svg
	echo "![destination_${state}](./media/destination_${state}.svg)" >>"${OUT_FILE}"

	echo "" >>"${OUT_FILE}"

	getDestinationStateTransitionRules "$state" | while read -r transition_rule; do
		echo Processing "$state" "$transition_rule"
		echo "* $(github_markdown_linkify "$transition_rule")" >>"${OUT_FILE}"
	done

	echo "" >>"${OUT_FILE}"
done
echo "" >>"${OUT_FILE}"

