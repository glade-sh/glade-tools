import { LightningElement } from "lwc";
import { gql, graphql } from "lightning/uiGraphQLApi";

export default class Oracle extends LightningElement {
  label = "lightning/uiGraphQLApi";
  query = gql`query AccountOracle { uiapi { query { Account { edges { node { Id Name { value } } } } } } }`;
  connectedCallback() {
    this.result = graphql ? "graphql ready" : "graphql missing";
  }
}
