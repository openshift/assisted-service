echo "## Transition Types" >>"${OUT_FILE}"
echo "Transition types are the events that can cause a state transition" >>"${OUT_FILE}"
echo "" >>"${OUT_FILE}"
for transition_type in $(jq '.transition_types | keys[]' -r "${JSON}"); do
    echo Processing "$transition_type"
    transition_type_name=$(getTransitionTypeName "$transition_type")
    transition_type_description=$(getTransitionTypeDescription "$transition_type")

    echo "### $transition_type_name" >>"${OUT_FILE}"
    echo "$transition_type_description" >>"${OUT_FILE}"
    echo "" >>"${OUT_FILE}"

    echo "#### Source states where this transition type applies" >>"${OUT_FILE}"
    for state in $(getTransitionTypeSourceStates "$transition_type"); do
        echo "* $(github_markdown_linkify "$(getStateName "$state")")" >>"${OUT_FILE}"
    done

    echo "" >>"${OUT_FILE}"

    echo "#### Destination states where this transition type applies" >>"${OUT_FILE}"
    for state in $(getTransitionTypeDestinationStates "$transition_type"); do
        echo "* $(github_markdown_linkify "$(getStateName "$state")")" >>"${OUT_FILE}"
    done

    echo "#### Transition rules using this transition type" >>"${OUT_FILE}"

    jq --arg transition_type "$transition_type" \
        --from-file "${SCRIPT_DIR}/queries/transition_type_transition_rules.jq" \
        "${JSON}" |
        smcat \
            --input-type json \
            --output-type svg \
            --direction left-right \
            --engine fdp \
            --output-to "${OUT_DIR}"/media/transition_type_"${transition_type}".svg
    echo "![transition_type_${transition_type}](./media/transition_type_${transition_type}.svg)" >>"${OUT_FILE}"

    echo "" >>"${OUT_FILE}"

    getTransitionTypeTransitionRules "$transition_type" | while read -r transition_rule; do
        echo Processing "$state" "$transition_rule"
        echo "* $(github_markdown_linkify "$transition_rule")" >>"${OUT_FILE}"
    done
done
echo "" >>"${OUT_FILE}"
