function getStateName() {
	jq --arg state "$1" '.states[$state].name' -r "${JSON}"
}

function getStateDescription() {
	jq --arg state "$1" '.states[$state].description' -r "${JSON}"
}

function getTransitionTypeName() {
	jq --arg transition_type "$1" '.transition_types[$transition_type].name' -r "${JSON}"
}

function getTransitionTypeDescription() {
	jq --arg transition_type "$1" '.transition_types[$transition_type].description' -r "${JSON}"
}

function getSourceStateTransitionTypes() {
	jq --arg state "$1" '[.transition_rules[] 
        | select(
            .source_states | index($state)
        ).transition_type] | sort | unique | .[]' -r "${JSON}"
}

function getSourceStateTransitionRules() {
	jq --arg state "$1" '[.transition_rules[] 
        | select(
            .source_states | index($state)
        ).name] | sort | unique | .[]' -r "${JSON}"
}

function getDestinationStateTransitionTypes() {
	jq --arg state "$1" '[.transition_rules[] 
        | select(
            .destination_state == $state
        ).transition_type] | sort | unique | .[]' -r "${JSON}"
}

function getDestinationStateTransitionRules() {
	jq --arg state "$1" '[.transition_rules[] 
        | select(
            .destination_state == $state
        ).name] | sort | unique | .[]' -r "${JSON}"
}

function getTransitionTypeSourceStates() {
	jq --arg transition_type "$1" '[.transition_rules[] 
        | select(
            .transition_type == $transition_type
        ).source_states[]] | sort | unique | .[]' -r "${JSON}"
}

function getTransitionTypeDestinationStates() {
	jq --arg transition_type "$1" '[.transition_rules[] 
        | select(
            .transition_type == $transition_type
        ).destination_state] | sort | unique | .[]' -r "${JSON}"
}

function getTransitionTypeTransitionRules() {
	jq --arg transition_type "$1" '[.transition_rules[] 
        | select(
            .transition_type == $transition_type
        ).name] | sort | unique | .[]' -r "${JSON}"
}

function github_markdown_linkify() {
	jq -n --arg name "$1" '$name 
        | gsub(" "; "-") 
        | gsub("[^a-zA-Z0-9-]"; "") 
        | ascii_downcase 
        | "[\($name)](#\(.))"
    ' -r
}
