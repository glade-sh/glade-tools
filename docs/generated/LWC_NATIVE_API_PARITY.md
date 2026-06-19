# Native LWC API Parity Ledger

Docs source: `../glade/example-projects/Salesforce Docs Scraper/salesforce-docs-expanded-run/lwc`

Rows: 157

## Status

| Status | Count |
| --- | ---: |
| `docs-only` | 15 |
| `local-only` | 99 |
| `partial-local` | 15 |
| `supported-local` | 27 |
| `unsupported-local` | 1 |

## Categories

| Category | Count |
| --- | ---: |
| `api-module` | 23 |
| `base-component` | 99 |
| `page-reference` | 16 |
| `salesforce-module` | 19 |

## Rows

| Category | Name | Status | Oracle | Evidence | Source | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| `api-module` | `experience/blockBuilderApi` | `docs-only` | `not-probed` |  | reference-api-modules.md | documented by Salesforce but no local shim is registered |
| `api-module` | `experience/cmsDeliveryApi` | `docs-only` | `not-probed` |  | reference-api-modules.md | documented by Salesforce but no local shim is registered |
| `api-module` | `experience/cmsEditorApi` | `docs-only` | `not-probed` |  | reference-api-modules.md | documented by Salesforce but no local shim is registered |
| `api-module` | `lightning/analyticsWaveApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/cmsDeliveryApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/conversationToolkitApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/empApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/empApi.js | reference-api-modules.md |  |
| `api-module` | `lightning/graphql` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/industriesEducationPublicApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/mobileCapabilities` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/platformUtilityBarApi` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/platformWorkspaceApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/platformWorkspaceApi.js | reference-api-modules.md |  |
| `api-module` | `lightning/serviceCloudVoiceToolkitApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/serviceKnowledgeApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/ | reference-api-modules.md | dynamic import family exists; individual member behavior must be probed |
| `api-module` | `lightning/uiAppsApi` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/uiGraphQLApi` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/uiLayoutApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/uiLayoutApi.js | reference-api-modules.md |  |
| `api-module` | `lightning/uiLearningPlatformApi` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/uiListApi` | `partial-local` | `not-probed` | /lightning/shims/lightning/uiListApi.js | reference-api-modules.md | deprecated module has a local diagnostic path, not full List UI parity |
| `api-module` | `lightning/uiListsApi` | `docs-only` | `not-probed` |  | reference-api-modules.md |  |
| `api-module` | `lightning/uiObjectInfoApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/uiObjectInfoApi.js | reference-api-modules.md |  |
| `api-module` | `lightning/uiRecordApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/uiRecordApi.js | reference-api-modules.md |  |
| `api-module` | `lightning/uiRelatedListApi` | `supported-local` | `not-probed` | /lightning/shims/lightning/uiRelatedListApi.js | reference-api-modules.md |  |
| `base-component` | `lightning/accordion` | `local-only` | `not-probed` | /lightning/shims/lightning/accordion.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/accordionSection` | `local-only` | `not-probed` | /lightning/shims/lightning/accordionSection.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/alert` | `local-only` | `not-probed` | /lightning/shims/lightning/alert.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/avatar` | `local-only` | `not-probed` | /lightning/shims/lightning/avatar.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/badge` | `local-only` | `not-probed` | /lightning/shims/lightning/badge.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/barcodeScanner` | `local-only` | `not-probed` | /lightning/shims/lightning/barcodeScanner.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/baseFormattedText` | `local-only` | `not-probed` | /lightning/shims/lightning/baseFormattedText.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/breadcrumb` | `local-only` | `not-probed` | /lightning/shims/lightning/breadcrumb.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/breadcrumbs` | `local-only` | `not-probed` | /lightning/shims/lightning/breadcrumbs.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/button` | `local-only` | `not-probed` | /lightning/shims/lightning/button.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/buttonGroup` | `local-only` | `not-probed` | /lightning/shims/lightning/buttonGroup.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/buttonIcon` | `local-only` | `not-probed` | /lightning/shims/lightning/buttonIcon.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/buttonIconStateful` | `local-only` | `not-probed` | /lightning/shims/lightning/buttonIconStateful.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/buttonMenu` | `local-only` | `not-probed` | /lightning/shims/lightning/buttonMenu.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/buttonStateful` | `local-only` | `not-probed` | /lightning/shims/lightning/buttonStateful.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/card` | `local-only` | `not-probed` | /lightning/shims/lightning/card.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/carousel` | `local-only` | `not-probed` | /lightning/shims/lightning/carousel.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/carouselImage` | `local-only` | `not-probed` | /lightning/shims/lightning/carouselImage.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/checkboxGroup` | `local-only` | `not-probed` | /lightning/shims/lightning/checkboxGroup.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/combobox` | `local-only` | `not-probed` | /lightning/shims/lightning/combobox.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/datatable` | `local-only` | `not-probed` | /lightning/shims/lightning/datatable.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/dialog` | `local-only` | `not-probed` | /lightning/shims/lightning/dialog.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/dualListbox` | `local-only` | `not-probed` | /lightning/shims/lightning/dualListbox.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/dynamicIcon` | `local-only` | `not-probed` | /lightning/shims/lightning/dynamicIcon.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/fileUpload` | `local-only` | `not-probed` | /lightning/shims/lightning/fileUpload.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/flow` | `local-only` | `not-probed` | /lightning/shims/lightning/flow.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/focusTrap` | `local-only` | `not-probed` | /lightning/shims/lightning/focusTrap.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedAddress` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedAddress.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedDateTime` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedDateTime.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedEmail` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedEmail.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedLocation` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedLocation.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedLookup` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedLookup.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedName` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedName.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedNumber` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedNumber.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedPhone` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedPhone.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedRichText` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedRichText.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedText` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedText.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedTime` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedTime.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/formattedUrl` | `local-only` | `not-probed` | /lightning/shims/lightning/formattedUrl.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/groupedCombobox` | `local-only` | `not-probed` | /lightning/shims/lightning/groupedCombobox.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/helptext` | `local-only` | `not-probed` | /lightning/shims/lightning/helptext.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/icon` | `local-only` | `not-probed` | /lightning/shims/lightning/icon.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/input` | `local-only` | `not-probed` | /lightning/shims/lightning/input.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/inputAddress` | `local-only` | `not-probed` | /lightning/shims/lightning/inputAddress.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/inputField` | `local-only` | `not-probed` | /lightning/shims/lightning/inputField.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/inputLocation` | `local-only` | `not-probed` | /lightning/shims/lightning/inputLocation.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/inputName` | `local-only` | `not-probed` | /lightning/shims/lightning/inputName.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/inputRichText` | `local-only` | `not-probed` | /lightning/shims/lightning/inputRichText.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/layout` | `local-only` | `not-probed` | /lightning/shims/lightning/layout.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/layoutItem` | `local-only` | `not-probed` | /lightning/shims/lightning/layoutItem.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/lookupAddress` | `local-only` | `not-probed` | /lightning/shims/lightning/lookupAddress.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/map` | `local-only` | `not-probed` | /lightning/shims/lightning/map.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/menuDivider` | `local-only` | `not-probed` | /lightning/shims/lightning/menuDivider.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/menuItem` | `local-only` | `not-probed` | /lightning/shims/lightning/menuItem.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/menuSubheader` | `local-only` | `not-probed` | /lightning/shims/lightning/menuSubheader.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/messages` | `local-only` | `not-probed` | /lightning/shims/lightning/messages.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/modal` | `local-only` | `not-probed` | /lightning/shims/lightning/modal.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/modalBody` | `local-only` | `not-probed` | /lightning/shims/lightning/modalBody.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/modalFooter` | `local-only` | `not-probed` | /lightning/shims/lightning/modalFooter.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/modalHeader` | `local-only` | `not-probed` | /lightning/shims/lightning/modalHeader.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/multiColumnSortingModal` | `local-only` | `not-probed` | /lightning/shims/lightning/multiColumnSortingModal.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/outputField` | `local-only` | `not-probed` | /lightning/shims/lightning/outputField.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/overlay` | `local-only` | `not-probed` | /lightning/shims/lightning/overlay.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/picklist` | `local-only` | `not-probed` | /lightning/shims/lightning/picklist.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/pill` | `local-only` | `not-probed` | /lightning/shims/lightning/pill.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/pillContainer` | `local-only` | `not-probed` | /lightning/shims/lightning/pillContainer.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/popup` | `local-only` | `not-probed` | /lightning/shims/lightning/popup.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/primitiveFigure` | `local-only` | `not-probed` | /lightning/shims/lightning/primitiveFigure.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/progressBar` | `local-only` | `not-probed` | /lightning/shims/lightning/progressBar.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/progressIndicator` | `local-only` | `not-probed` | /lightning/shims/lightning/progressIndicator.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/progressRing` | `local-only` | `not-probed` | /lightning/shims/lightning/progressRing.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/progressStep` | `local-only` | `not-probed` | /lightning/shims/lightning/progressStep.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/prompt` | `local-only` | `not-probed` | /lightning/shims/lightning/prompt.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/quickActionPanel` | `local-only` | `not-probed` | /lightning/shims/lightning/quickActionPanel.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/radioGroup` | `local-only` | `not-probed` | /lightning/shims/lightning/radioGroup.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/recordEditForm` | `local-only` | `not-probed` | /lightning/shims/lightning/recordEditForm.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/recordForm` | `local-only` | `not-probed` | /lightning/shims/lightning/recordForm.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/recordPicker` | `local-only` | `not-probed` | /lightning/shims/lightning/recordPicker.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/recordViewForm` | `local-only` | `not-probed` | /lightning/shims/lightning/recordViewForm.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/relativeDateTime` | `local-only` | `not-probed` | /lightning/shims/lightning/relativeDateTime.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/select` | `local-only` | `not-probed` | /lightning/shims/lightning/select.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/slider` | `local-only` | `not-probed` | /lightning/shims/lightning/slider.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/spinner` | `local-only` | `not-probed` | /lightning/shims/lightning/spinner.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/stackedTab` | `local-only` | `not-probed` | /lightning/shims/lightning/stackedTab.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/stackedTabset` | `local-only` | `not-probed` | /lightning/shims/lightning/stackedTabset.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/tab` | `local-only` | `not-probed` | /lightning/shims/lightning/tab.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/tabset` | `local-only` | `not-probed` | /lightning/shims/lightning/tabset.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/textarea` | `local-only` | `not-probed` | /lightning/shims/lightning/textarea.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/tile` | `local-only` | `not-probed` | /lightning/shims/lightning/tile.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/toast` | `local-only` | `not-probed` | /lightning/shims/lightning/toast.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/toastContainer` | `local-only` | `not-probed` | /lightning/shims/lightning/toastContainer.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/tree` | `local-only` | `not-probed` | /lightning/shims/lightning/tree.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/treeGrid` | `local-only` | `not-probed` | /lightning/shims/lightning/treeGrid.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigation` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigation.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigationItem` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigationItem.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigationItemBadge` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigationItemBadge.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigationItemIcon` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigationItemIcon.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigationOverflow` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigationOverflow.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `base-component` | `lightning/verticalNavigationSection` | `local-only` | `not-probed` | /lightning/shims/lightning/verticalNavigationSection.js |  | local base-component implementation; supplied LWC docs scrape does not include Component Reference rows |
| `page-reference` | `comm__loginPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `comm__managedContentPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `comm__namedPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__app` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__component` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__externalRecordPage` | `docs-only` | `not-probed` |  | reference-page-reference-type.md | not implemented by the local navigation service |
| `page-reference` | `standard__externalRecordRelationshipPage` | `docs-only` | `not-probed` |  | reference-page-reference-type.md | not implemented by the local navigation service |
| `page-reference` | `standard__flow` | `docs-only` | `not-probed` |  | reference-page-reference-type.md | not implemented by the local navigation service |
| `page-reference` | `standard__knowledgeArticlePage` | `docs-only` | `not-probed` |  | reference-page-reference-type.md | not implemented by the local navigation service |
| `page-reference` | `standard__namedPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__navItemPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__objectPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__quickAction` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__recordPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__recordRelationshipPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `page-reference` | `standard__webPage` | `supported-local` | `not-probed` | lwcruntime/src/shell/navigation-service.mjs | reference-page-reference-type.md |  |
| `salesforce-module` | `@salesforce/apex/` | `supported-local` | `not-probed` | /lightning/shims/apex/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/apexContinuation` | `docs-only` | `not-probed` |  | reference-salesforce-modules.md | documented by Salesforce but no local shim is registered |
| `salesforce-module` | `@salesforce/community/` | `partial-local` | `not-probed` | /lightning/shims/community/ | reference-salesforce-modules.md | only basePath and Id are modeled |
| `salesforce-module` | `@salesforce/community/Id` | `supported-local` | `not-probed` | /lightning/shims/community/Id.js | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/community/basePath` | `supported-local` | `not-probed` | /lightning/shims/community/basePath.js | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/contentAssetUrl/` | `supported-local` | `not-probed` | /lightning/shims/contentAssetUrl/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/customPermission/` | `supported-local` | `not-probed` | /lightning/shims/customPermission/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/i18n/` | `partial-local` | `not-probed` | /lightning/shims/i18n/ | reference-salesforce-modules.md | common locale values are modeled; full org locale matrix is not probed |
| `salesforce-module` | `@salesforce/i18n/dir` | `partial-local` | `not-probed` | /lightning/shims/i18n/ | reference-salesforce-modules.md | dynamic import family exists; individual member behavior must be probed |
| `salesforce-module` | `@salesforce/i18n/lang` | `partial-local` | `not-probed` | /lightning/shims/i18n/ | reference-salesforce-modules.md | dynamic import family exists; individual member behavior must be probed |
| `salesforce-module` | `@salesforce/label/` | `supported-local` | `not-probed` | /lightning/shims/label/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/resourceUrl/` | `supported-local` | `not-probed` | /lightning/shims/resourceUrl/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/schema/` | `supported-local` | `not-probed` | /lightning/shims/schema/ | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/site/` | `partial-local` | `not-probed` | /lightning/shims/site/ | reference-salesforce-modules.md | only Id is modeled |
| `salesforce-module` | `@salesforce/site/Id` | `supported-local` | `not-probed` | /lightning/shims/site/Id.js | reference-salesforce-modules.md |  |
| `salesforce-module` | `@salesforce/site/activeLanguages` | `unsupported-local` | `not-probed` | /lightning/shims/site/ | reference-salesforce-modules.md | local site shim returns unsupported for activeLanguages |
| `salesforce-module` | `@salesforce/user/Id` | `partial-local` | `not-probed` | /lightning/shims/user/ | reference-salesforce-modules.md | dynamic import family exists; individual member behavior must be probed |
| `salesforce-module` | `@salesforce/user/isGuest` | `partial-local` | `not-probed` | /lightning/shims/user/ | reference-salesforce-modules.md | dynamic import family exists; individual member behavior must be probed |
| `salesforce-module` | `@salesforce/userPermission/` | `docs-only` | `not-probed` |  | reference-salesforce-modules.md | no local user permission shim is registered |

## Next Gates

- **live org oracle**: `glade-tools lwc capture --target-org oaer-probe-max --project <fixture> --local-browser-capture --browser-capture --out <capture.json>`
- **expand docs source**: `refresh the LWC docs scrape with Component Reference and per-module API pages, then rerun glade-tools lwc parity --docs <lwc-docs>`
- **local parity check**: `glade-tools lwc parity --docs <lwc-docs> --check docs/generated/LWC_NATIVE_API_PARITY.md`
