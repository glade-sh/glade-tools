import { LightningElement } from "lwc";
import * as api from "lightning/platformWorkspaceApi";

export default class Oracle extends LightningElement {
  label = "lightning/platformWorkspaceApi";
  exports = Object.keys(api || {}).join(",");
}
