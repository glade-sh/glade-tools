import { LightningElement } from "lwc";
import * as api from "lightning/conversationToolkitApi";

export default class Oracle extends LightningElement {
  label = "lightning/conversationToolkitApi";
  exports = Object.keys(api || {}).join(",");
}
