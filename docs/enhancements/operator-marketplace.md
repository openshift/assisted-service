# Assisted Installer Operator Marketplace

## Summary

This enhancement introduces two new API endpoints to the Assisted Installer Operator marketplace:

1. **List Bundles**: Retrieves a list of all available bundles (e.g., Virtualization, OCP Migration).
2. **List Operators by Bundle**: Retrieves a list of operators associated with a specified bundle.

These additions aim to enhance the UX of the operators tab by enabling better organization through bundles, categories, and a search mechanism.

## Motivation

The number of OLM operators in the Assisted Installer is rapidly increasing. To maintain usability, a reconsideration of the Operators tab UX is required. By introducing bundles and categories, the API will allow for better grouping and retrieval of operators, ultimately improving user experience and accessibility.

### Goals

- Implement an endpoint to list all bundles.
- Implement an endpoint to list operators within a specified bundle.
- Update the API documentation (Swagger/OpenAPI) to reflect these changes.

### Non-Goals

- Modifying existing operator functionalities.
- Implementing UI elements (this is handled separately).
- Adding bundle management features such as creation or deletion.

## Proposal

### User Stories

#### Story 1

As an API consumer, I want to retrieve a list of all available bundles so that I can understand the available groupings of operators.

#### Story 2

As an API consumer, I want to retrieve a list of operators associated with a specific bundle so that I can manage or interact with them accordingly.

### Implementation Details/Notes/Constraints

- **List Bundles Endpoint**:

  - **Method**: GET
  - **Path**: `/v2/operators/v1/bundles`
  - **Response**: JSON array of bundles names.

- **List Operators by Bundle Endpoint**:

  - **Method**: GET
  - **Path**: `/v2/operators/v1/bundles/{bundle_name}`
  - **Parameters**:
    - `bundle_name` (path parameter): Identifier of the bundle.
  - **Response**: JSON array of operators namees.

- **Swagger/OpenAPI Documentation**: Update the API documentation to include these new endpoints with appropriate request and response schemas.

- **Operator Entity Update**: The operator entity in the backend will now have a `bundles` attribute, which will contain a list of all bundles the operator belongs to.

### Existing and Planned Operators

#### Existing Operators

- OpenShift Virtualization (CNV)
- Multicluster Engine (MCE)
- Logical Volume Manager Storage (LVMS)
- OpenShift Data Foundation (ODF)
- Local Storage Operator (LSO) - Not currently in the UI

#### Operators to be added
- MTV (2.36)
- OpenShift AI (2.37)
- Nvidia GPU (2.37) - Not currently in the UI
- Pipelines (2.37) - Not currently in the UI
- Serverless (2.37) - Not currently in the UI
- ServiceMesh (2.37) - Not currently in the UI

#### Work In Progress (WIP) Operators

- OpenShift Service Connect (OSC, CoCo)
- Authorino

#### Bundles

The initial implementation will introduce two bundles:

1. OpenShift AI
2. Virtualization

These bundles will begin with the existing dependencies of the core operator. Adding operators to a bundle will be straightforward by assigning the bundle to the operator's bundle attribute.

### Open Questions

### UI Impact

To improve the user experience and support a growing number of operators, the Operators step in the Assisted Installer will undergo significant redesign. This includes organizing operators into bundles, enabling layered views, and introducing search functionality. The new design will allow users to interact more effectively with the available options, as follows:

- **Bundled View**: Operators will be grouped into logical bundles (e.g., Virtualization, OCP Migration), simplifying navigation and selection.

- **Search Functionality**: Users will be able to search for specific operators or bundles. Results will be categorized into bundles and individual operators.

- **Selection Mechanics**:

  - Selecting a bundle automatically selects all operators within that bundle.
  - Operators with dependencies will display as selected but disabled (greyed out) to prevent deselection unless the dependency is removed.
  - Optional/recommended operators within a bundle will be pre-selected but can be deselected by users.
  - Deselecting operators within a bundle will update the bundle's selection state to reflect a partial selection.
  - Selecting an individual operator with dependencies will also select its dependent operators, which cannot be deselected independently.


### Test Plan

- **Unit Tests**: Validate the functionality of the new endpoints, including various scenarios such as empty bundles and invalid bundle IDs.
- **Integration Tests**: Ensure the endpoints integrate seamlessly with existing API functionalities.
