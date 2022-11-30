# Keep a copy of the root node for future use
. as $root |

# We only care about transitions for which our state is the destination state
[
    .transition_rules[]
    | select(.destination_state == $state)
] as $relevant_rules |

# Create the actual graph JSON
{
    "states": [
        [
            $relevant_rules[]
            | .source_states[]
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
        | .source_state = .source_states[]
        | {
            "from": .source_state,
            "to": .destination_state,
            "label": "\($root.transition_types[.transition_type].name) - \(.name)",
        }
    ]
}
