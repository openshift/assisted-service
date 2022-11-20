# Keep a copy of the root node for future use
. as $root |

# We only care about transitions for which our state is a source state
[
    .transition_rules[]
    | .source_state = .source_states[]
    | select(.source_state == $state)
] as $relevant_rules |

# Create the actual graph JSON
{
    "states": [
        [
            $relevant_rules[]
            | .destination_state
        ] + [$state]
        | sort
        | unique[]
        | {
            "name": .,
            "type": "regular"
        }
    ],
    "transitions": [
        $relevant_rules[]
        | {
            "from": .source_state,
            "to": .destination_state,
            "label": "\($root.transition_types[.transition_type].name) - \(.name)",
        }
    ]
}
