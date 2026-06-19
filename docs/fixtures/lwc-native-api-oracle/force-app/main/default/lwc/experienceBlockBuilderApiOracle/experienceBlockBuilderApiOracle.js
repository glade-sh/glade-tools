import { LightningElement } from "lwc";
import * as api from "experience/blockBuilderApi";

export default class Oracle extends LightningElement {
  label = "experience/blockBuilderApi";
  exports = Object.keys(api || {}).join(",");
}
