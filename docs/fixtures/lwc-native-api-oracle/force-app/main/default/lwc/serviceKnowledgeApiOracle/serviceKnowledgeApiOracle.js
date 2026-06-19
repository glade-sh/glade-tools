import { LightningElement } from "lwc";
import * as api from "lightning/serviceKnowledgeApi";

export default class Oracle extends LightningElement {
  label = "lightning/serviceKnowledgeApi";
  exports = Object.keys(api || {}).join(",");
}
